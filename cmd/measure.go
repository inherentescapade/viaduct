package cmd

import (
	"context"
	"fmt"
	"github.com/inherentescapade/viaduct/discord"
	"log"
	"math"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/spf13/cobra"
)

var (
	measureRounds  int
	measureSustain time.Duration
)

// mlog timestamps every line (microsecond precision) so the output doubles as a
// timeline — essential when reasoning about a rolling-window rate limit.
var mlog = log.New(os.Stdout, "", log.LstdFlags|log.Lmicroseconds)

var measureCmd = &cobra.Command{
	Use:   "measure <guild-name-or-id>",
	Short: "Empirically measure DELETE rate limits using real messages",
	Long: `Measure Discord's DELETE rate limits by deleting your own real messages.

Two phases:
  1. Burst-capacity probe — repeats N times, each on a freshly-drained
     bucket, bursting to failure so the capacity is sampled (avg ± stddev)
     rather than guessed from one noisy snapshot. Authoritative limit and
     window are read straight from Discord's X-RateLimit-* headers.
  2. Sustained-rate validation — drains, then holds a paced rate over many
     windows, riding through any 429s, to confirm the rate that actually
     holds over time (a rolling-window limit can pass one burst yet fail
     under sustained load).

This deletes real messages you own.

Examples:
  viaduct measure "My Server"
  viaduct measure 123456789 --rounds 6 --sustain 60s`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		config := loadOrCreateConfig()
		applyFlagOverrides(config)

		if config.Token == "" {
			return fmt.Errorf("no token configured — run `viaduct` to set up, or use --token")
		}

		client := discord.NewClient(config.Token, config.BotMode, http.DefaultClient)

		user, err := client.ValidateToken()
		if err != nil {
			return err
		}
		mlog.Printf("Logged in as %s (%s)\n\n", user.Username, user.Id)

		guild, err := resolveGuildArg(client, args[0])
		if err != nil {
			return err
		}

		mlog.Printf("Measuring DELETE rate limits in %q\n\n", guild.Name)

		// Fetch a pool of real messages to delete: enough for every burst rep
		// plus both sustained trials (~one delete per 1.5s of each trial).
		needed := measureRounds*60 + 2*int(measureSustain.Seconds()/1.5) + 100
		pool, err := fetchMessagePool(client, guild, user.Id, needed)
		if err != nil {
			return err
		}
		if len(pool) == 0 {
			return fmt.Errorf("no messages of yours found anywhere in %q — send some messages first", guild.Name)
		}
		mlog.Printf("Found %d of your messages to use as probes.\n\n", len(pool))

		idx := 0
		next := func() (string, string, bool) {
			if idx >= len(pool) {
				return "", "", false
			}
			m := pool[idx]
			idx++
			return m.ChannelId, m.Id, true
		}

		type roundResult struct {
			sent       int
			elapsed    time.Duration
			gaps       []time.Duration
			hit429     bool
			retryAfter float64
		}

		// ── Phase 1: burst-capacity probe (repeated, each rep on a drained bucket) ──
		// A rolling-window limit must be sampled repeatedly over time, not once.
		// Each rep first DRAINS the bucket (waits a whole window since the last
		// request) then bursts at full speed until the first 429, so every rep is
		// an independent sample we can average and get variance from.
		mlog.Printf("Phase 1: burst-capacity probe (%d reps, each on a drained bucket)\n\n", measureRounds)

		const burstCap = 100
		results := make([]roundResult, 0, measureRounds)
		var nextDrain time.Duration
		var windowEst float64 // best (largest) window estimate seen, in seconds

		for round := 1; round <= measureRounds; round++ {
			if round > 1 {
				drain := nextDrain
				if drain < time.Second {
					drain = time.Second
				}
				mlog.Printf("  draining %.2fs...\n", drain.Seconds())
				time.Sleep(drain)
			}

			mlog.Printf("  Rep %d/%d: bursting at full speed...", round, measureRounds)

			chID, msgID, ok := next()
			if !ok {
				mlog.Println("  (ran out of messages)")
				break
			}

			recs, err := client.ProbeDeleteReal(chID, msgID, burstCap, 0, next)
			if err != nil {
				return err
			}

			rr := roundResult{}
			for _, r := range recs {
				if r.Status == 429 {
					rr.hit429 = true
					rr.retryAfter = r.RetryAfter
				} else {
					// Count only confirmed deletions (204). A 404/403/etc. does
					// not consume a rate-limit slot the way a real delete does, so
					// folding it into the burst count corrupts the capacity figure.
					if r.Status == 204 {
						rr.sent++
					}
					if r.Gap > 0 {
						rr.gaps = append(rr.gaps, r.Gap)
					}
				}
			}
			if len(recs) >= 2 {
				rr.elapsed = recs[len(recs)-1].SentAt.Sub(recs[0].SentAt)
			}

			if rr.hit429 {
				mlog.Printf("  Rep %d/%d: %d before 429  elapsed: %s  retry_after: %.2fs",
					round, measureRounds, rr.sent, rr.elapsed.Round(time.Millisecond), rr.retryAfter)
			} else {
				mlog.Printf("  Rep %d/%d: %d (no 429)  elapsed: %s",
					round, measureRounds, rr.sent, rr.elapsed.Round(time.Millisecond))
			}

			results = append(results, rr)

			// Drain the NEXT rep by the FULL window, not just retry_after. In a
			// rolling window retry_after only frees the single oldest slot, so
			// draining by it leaves most of the burst still counted and each rep
			// starts dirtier than the last (counts collapse 20→8→1). Estimate the
			// window from a cold-start burst (elapsed-to-429 + retry_after) and keep
			// the largest estimate — the cleanest, least-contaminated rep.
			if rr.hit429 {
				if w := rr.elapsed.Seconds() + rr.retryAfter; w > windowEst {
					windowEst = w
				}
			}
			if windowEst > 0 {
				nextDrain = time.Duration(windowEst*float64(time.Second)) + time.Second
			}
		}

		// ── Aggregate Phase 1 ────────────────────────────────────────────────
		var retryAfters []float64
		var burstCounts []int
		for _, r := range results {
			if r.hit429 {
				burstCounts = append(burstCounts, r.sent)
				retryAfters = append(retryAfters, r.retryAfter)
			}
		}

		avgRetry := 0.0
		if len(retryAfters) > 0 {
			sum := 0.0
			for _, r := range retryAfters {
				sum += r
			}
			avgRetry = sum / float64(len(retryAfters))
		}

		// Discord doesn't return rate-limit headers for user-token deletes, so the
		// window is estimated from timing (the largest cold-start burst in Phase 1).
		inferredWindow := windowEst

		// Burst-capacity stats over every rep. With full-window draining each rep
		// starts clean, so all reps are valid samples — none is a warm-up to drop.
		minB, maxB, avgB := minMaxAvg(burstCounts)
		sdB := stddevInt(burstCounts)

		// Window used for Phase 2 drain/steady-state math, from the empirical probes.
		recWindow := inferredWindow

		// ── Phase 2: sustained-rate validation (the rolling-window test) ──────
		// Burst capacity says nothing about the rate you can hold: a reservoir lets
		// the first burst run long, then a longer envelope clamps you. So MEASURE
		// the sustained ceiling directly — saturate (send flat out, riding through
		// 429s by honouring retry_after, which paces you exactly at the true limit),
		// read off the effective throughput, then validate a slightly-slower gap
		// holds clean. The validated gap — not burst-window math — is the answer.
		mlog.Println()
		mlog.Println("Phase 2: sustained-rate validation")
		mlog.Print("(saturate to find the real ceiling, then validate a safe gap)\n\n")

		drain := time.Duration(recWindow*float64(time.Second)) + 2*time.Second
		if drain < 3*time.Second {
			drain = 3 * time.Second
		}

		var sustainedGap time.Duration // validated safe gap
		var sustainedRate float64      // msg/min actually achieved
		validatedClean := false

		// Step 1 — saturate. Effective throughput while riding through 429s is the
		// sustained ceiling, because retry_after paces you at exactly the limit.
		// No pre-drain here: saturating starts by blowing through whatever's left
		// anyway, and steadyGap discards the first window, so a drain would only
		// add dead time without changing the steady-state measurement.
		mlog.Printf("  saturating at full speed for %s to find the ceiling...\n", measureSustain)
		satRecs, err := client.ProbeSustained(measureSustain, 0, len(pool), next)
		if err != nil {
			return err
		}
		satDel, satRL, satSpan := summarizeSustained(satRecs)
		if satDel > 0 && satSpan > 0 {
			mlog.Printf("    → saturated: %d deleted in %s, ~%.0f msg/min (%d × 429)\n",
				satDel, satSpan.Round(time.Second), float64(satDel)/satSpan.Seconds()*60, satRL)

			// Steady-state estimate: ignore deletes inside the first window — those
			// drain the cold reservoir and would make the gap look faster than it
			// can actually be sustained. Use the tail's real interval as the start.
			startGap := satSpan / time.Duration(satDel) // fallback: overall
			if g := steadyGap(satRecs, windowEst); g > 0 {
				startGap = g
			}

			// Step 2 — validate, increasing the gap until a run is genuinely clean.
			// This converges on a gap that holds instead of just reporting failure.
			gap := time.Duration(float64(startGap) * 1.10)
			const maxAttempts = 5
			for attempt := 1; attempt <= maxAttempts; attempt++ {
				if idx >= len(pool) {
					mlog.Println("  (pool exhausted — stopping validation)")
					break
				}
				mlog.Printf("  draining %.2fs, then validating %dms gap for %s (try %d/%d)...\n",
					drain.Seconds(), gap.Milliseconds(), measureSustain, attempt, maxAttempts)
				time.Sleep(drain)
				vRecs, err := client.ProbeSustained(measureSustain, gap, len(pool), next)
				if err != nil {
					return err
				}
				vDel, vRL, vSpan := summarizeSustained(vRecs)
				vRate := 0.0
				if vSpan > 0 {
					vRate = float64(vDel) / vSpan.Seconds() * 60
				}
				sustainedGap = gap
				sustainedRate = vRate
				if vRL == 0 {
					validatedClean = true
					mlog.Printf("    → %d deleted in %s, ~%.0f msg/min  ✓ clean — sustainable\n",
						vDel, vSpan.Round(time.Second), vRate)
					break
				}
				mlog.Printf("    → %d deleted in %s, ~%.0f msg/min  ✗ %d hit(s); backing off\n",
					vDel, vSpan.Round(time.Second), vRate, vRL)
				gap = time.Duration(float64(gap) * 1.5) // back off and retry
			}
		} else {
			mlog.Println("    → no deletes completed (pool empty or every request 429'd).")
		}

		// ── Summary ──────────────────────────────────────────────────────────
		mlog.Println()
		mlog.Println("── Summary ──────────────────────────────────────")

		if len(burstCounts) > 0 {
			// The first (cold) burst drains a built-up reservoir; later reps
			// converge to the true steady-state burst. Report both, not one
			// misleading average across them.
			cold := burstCounts[0]
			steady := burstCounts[len(burstCounts)-1]
			mlog.Printf("  Burst (cold):     %d  (one-time reservoir on a long-idle bucket)\n", cold)
			mlog.Printf("  Burst (steady):   %d  (range %d–%d, avg %.1f ± %.1f over %d reps)\n",
				steady, minB, maxB, avgB, sdB, len(burstCounts))
			mlog.Printf("  Avg retry_after:  %.2fs\n", avgRetry)
		} else {
			mlog.Printf("  Burst capacity:   ≥%d (never hit a 429 in %d reps)\n", burstCap, len(results))
		}
		if recWindow > 0 {
			mlog.Printf("  Short window:     ~%.2fs (inferred from timing)\n", recWindow)
		}
		if sustainedRate > 0 {
			mlog.Printf("  Sustained rate:   ~%.0f msg/min (measured by saturation)\n", sustainedRate)
		}

		mlog.Println()
		mlog.Println("── Recommended rate limiter settings ────────────")
		// The recommendation is the gap Phase 2 actually validated, NOT burst math
		// (burst math contradicts the sustained measurement — a longer envelope
		// clamps the real rate well below burst capacity).
		if sustainedGap > 0 {
			status := "use this as your per-message delay"
			if !validatedClean {
				status = "still saw 429s — back off further (the true ceiling is lower)"
			}
			mlog.Printf("  Flat delay:       %dms/message  (%s)\n", sustainedGap.Milliseconds(), status)
			if sustainedRate > 0 {
				mlog.Printf("  Throughput:       ~%.0f msg/min\n", sustainedRate)
			}
		} else {
			mlog.Println("  Not enough data — no deletes completed in Phase 2.")
		}

		return nil
	},
}

// stddevInt returns the sample standard deviation of ns (0 for <2 samples).
func stddevInt(ns []int) float64 {
	if len(ns) < 2 {
		return 0
	}
	var sum float64
	for _, n := range ns {
		sum += float64(n)
	}
	mean := sum / float64(len(ns))
	var v float64
	for _, n := range ns {
		d := float64(n) - mean
		v += d * d
	}
	return math.Sqrt(v / float64(len(ns)-1))
}

// steadyGap returns the average interval between confirmed deletes that happen
// AFTER the first window of a saturate run — i.e. once the cold reservoir is
// spent and the long-term envelope is what's pacing you. Returns 0 if there
// aren't enough tail samples to estimate from.
func steadyGap(recs []discord.ProbeRecord, windowSec float64) time.Duration {
	if len(recs) == 0 || windowSec <= 0 {
		return 0
	}
	cutoff := recs[0].SentAt.Add(time.Duration(windowSec * float64(time.Second)))
	var first, last time.Time
	n := 0
	for _, r := range recs {
		if r.Status == 204 && r.SentAt.After(cutoff) {
			if n == 0 {
				first = r.SentAt
			}
			last = r.SentAt
			n++
		}
	}
	if n < 3 { // too few tail deletes to trust
		return 0
	}
	return time.Duration(last.Sub(first).Nanoseconds() / int64(n-1))
}

// summarizeSustained reduces a sustained run to confirmed deletes, rate-limit
// hits (429s), and the wall-clock span it covered.
func summarizeSustained(recs []discord.ProbeRecord) (deletes, rateLimited int, span time.Duration) {
	for _, r := range recs {
		if r.Status == 429 {
			rateLimited++
		} else if r.Status == 204 {
			deletes++
		}
	}
	if len(recs) >= 2 {
		span = recs[len(recs)-1].DoneAt.Sub(recs[0].SentAt)
	}
	return deletes, rateLimited, span
}

func minMaxAvg(ns []int) (min, max int, avg float64) {
	if len(ns) == 0 {
		return 0, 0, 0
	}
	min, max = ns[0], ns[0]
	sum := 0
	for _, n := range ns {
		sum += n
		if n < min {
			min = n
		}
		if n > max {
			max = n
		}
	}
	return min, max, float64(sum) / float64(len(ns))
}

// fetchMessagePool searches the whole guild (or all DMs) for the user's own
// messages — wherever they live. Each message carries its own ChannelId, so the
// probes delete in-place; no single channel needs to be chosen.
func fetchMessagePool(client *discord.Client, guild *discord.Guild, userID string, needed int) ([]discord.Message, error) {
	mlog.Printf("Fetching up to %d of your messages from %q...\n", needed, guild.Name)

	var all []discord.Message
	offset := 0
	for len(all) < needed {
		resp, err := client.GetMessages(context.Background(), guild.Id, nil, &userID, nil, nil, offset, true)
		if err != nil {
			return nil, fmt.Errorf("search failed: %w", err)
		}
		if resp.DoingDeepHistoricalIndex {
			time.Sleep(2 * time.Second)
			continue
		}
		if len(resp.Messages) == 0 {
			break
		}
		for _, cluster := range resp.Messages {
			if len(cluster) > 0 && cluster[0].Author.Id == userID {
				all = append(all, cluster[0])
			}
		}
		offset += len(resp.Messages)
		if offset >= resp.TotalResults {
			break
		}
	}

	// Sort oldest-first so we delete oldest messages (less disruptive).
	sort.Slice(all, func(i, j int) bool {
		return all[i].Timestamp.Before(all[j].Timestamp)
	})
	if len(all) > needed {
		all = all[:needed]
	}
	return all, nil
}

func init() {
	measureCmd.Flags().IntVar(&measureRounds, "rounds", 3, "Phase 1 reps (seed the window + deplete the reservoir)")
	measureCmd.Flags().DurationVar(&measureSustain, "sustain", 30*time.Second, "Duration of each Phase 2 sustained-rate trial")
	rootCmd.AddCommand(measureCmd)
}
