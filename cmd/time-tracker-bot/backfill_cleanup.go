package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/username/time-tracker-bot/internal/config"
	"github.com/username/time-tracker-bot/internal/tracker"
	"github.com/username/time-tracker-bot/pkg/dateutil"
	"go.uber.org/zap"
)

func backfillCmd() *cobra.Command {
	var fromStr string
	var toStr string
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "backfill",
		Short: "Fill missing work days with time entries",
		Long:  "Backfill missing work days in the period. Uses 120% coverage algorithm to find all relevant issues.",
		RunE: func(cmd *cobra.Command, args []string) error {
			var from, to time.Time
			var err error

			// Default: backfill current month (excluding today)
			if fromStr == "" && toStr == "" {
				now := dateutil.Today()
				from = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)
				to = now.AddDate(0, 0, -1) // yesterday
			} else {
				if fromStr == "" || toStr == "" {
					return fmt.Errorf("both --from and --to must be specified")
				}
				from, err = dateutil.ParseDate(fromStr)
				if err != nil {
					return fmt.Errorf("invalid from date: %w", err)
				}
				to, err = dateutil.ParseDate(toStr)
				if err != nil {
					return fmt.Errorf("invalid to date: %w", err)
				}
			}

			// Load config
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			cfg.ExpandEnvVars()

			// Initialize components
			manager, err := initializeManager(cfg)
			if err != nil {
				return err
			}

			logger.Info("Starting backfill",
				zap.Time("from", from),
				zap.Time("to", to),
				zap.Bool("dry_run", dryRun))

			// Run backfill
			result, err := manager.BackfillPeriod(from, to, dryRun)
			if err != nil {
				return fmt.Errorf("backfill failed: %w", err)
			}

			// Print results
			fmt.Printf("\n📋 Backfill Summary (%s to %s):\n", from.Format("2006-01-02"), to.Format("2006-01-02"))
			fmt.Println("═══════════════════════════════════════════════════════")
			fmt.Printf("  Processed days:    %d\n", result.ProcessedDays)
			fmt.Printf("  Total entries:     %d\n", result.TotalEntries)
			fmt.Printf("  Total time:        %.1fh (%.0f minutes)\n",
				result.TotalMinutes/60, result.TotalMinutes)

			if len(result.DayResults) > 0 {
				fmt.Println("\n  Days processed:")
				for _, dayResult := range result.DayResults {
					if dayResult.Success {
						fmt.Printf("    ✅ %s: %d entries, %.1fh\n",
							dayResult.Date.Format("2006-01-02"),
							len(dayResult.Entries),
							dayResult.TotalMinutes/60)

						// Show first 3 entries as preview
						shown := 0
						for _, entry := range dayResult.Entries {
							if shown >= 3 {
								break
							}
							hours := int(entry.Minutes / 60)
							mins := int(entry.Minutes) % 60
							fmt.Printf("      • %-15s  %2dh %2dm  %s\n",
								entry.IssueKey, hours, mins, entry.Comment)
							shown++
						}
					} else {
						fmt.Printf("    ❌ %s: failed\n", dayResult.Date.Format("2006-01-02"))
					}
				}
			}

			if dryRun {
				fmt.Println("\n[DRY RUN] No worklogs were created")
			} else {
				fmt.Println("\n✅ Backfill completed successfully")
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&fromStr, "from", "", "Start date (YYYY-MM-DD, default: first day of current month)")
	cmd.Flags().StringVar(&toStr, "to", "", "End date (YYYY-MM-DD, default: yesterday)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be done without creating worklogs")

	return cmd
}

func cleanupCmd() *cobra.Command {
	var dateStr string
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "cleanup",
		Short: "Remove duplicate/excess worklogs for a date",
		Long:  "Detect and remove duplicate worklogs (same issue + description). Also normalizes overage by removing largest entries.",
		RunE: func(cmd *cobra.Command, args []string) error {
			var date time.Time
			var err error

			if dateStr == "today" {
				date = dateutil.Today()
			} else if dateStr == "yesterday" {
				date = dateutil.Yesterday()
			} else {
				date, err = dateutil.ParseDate(dateStr)
				if err != nil {
					return fmt.Errorf("invalid date format: %w", err)
				}
			}

			// Load config
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			cfg.ExpandEnvVars()

			// Initialize components
			manager, err := initializeManager(cfg)
			if err != nil {
				return err
			}

			logger.Info("Starting cleanup",
				zap.Time("date", date),
				zap.Bool("dry_run", dryRun))

			// Get worklogs
			trackerClient := manager.GetTrackerClient()
			worklogs, err := trackerClient.GetWorklogsForToday(date)
			if err != nil {
				return fmt.Errorf("failed to get worklogs: %w", err)
			}

			if len(worklogs) == 0 {
				fmt.Printf("\nNo worklogs found for %s\n", date.Format("2006-01-02"))
				return nil
			}

			// Calculate total time
			totalMinutes := 0.0
			for _, wl := range worklogs {
				minutes, err := tracker.ParseISO8601Duration(wl.Duration)
				if err != nil {
					logger.Warn("Failed to parse duration", zap.String("duration", wl.Duration))
					continue
				}
				totalMinutes += minutes
			}

			// Check threshold
			_, targetHours, err := manager.GetCalendar().IsWorkday(date)
			if err != nil {
				return fmt.Errorf("failed to check workday: %w", err)
			}

			targetMinutes := float64(targetHours * 60)
			threshold := targetMinutes * 1.05

			// Declare variables before any goto
			var toKeep []tracker.Worklog
			var toDelete []tracker.Worklog
			var keptMinutes float64
			var deletedCount int

			fmt.Printf("\n🔍 Cleanup Analysis for %s:\n", date.Format("2006-01-02"))
			fmt.Println("═══════════════════════════════════════════════════════")
			fmt.Printf("  Total worklogs:   %d entries\n", len(worklogs))
			fmt.Printf("  Total time:       %.1fh (%.0f minutes)\n", totalMinutes/60, totalMinutes)
			fmt.Printf("  Target time:      %.1fh (%.0f minutes)\n", targetMinutes/60, targetMinutes)
			fmt.Printf("  Threshold:        %.1fh (%.0f minutes)\n", threshold/60, threshold)

			// If exactly at target, nothing to do
			if totalMinutes == targetMinutes {
				fmt.Println("\n✅ Already at exact target time")
				return nil
			}

			// If within threshold but not exact, skip duplicate detection
			if totalMinutes <= threshold {
				fmt.Printf("\n⚙️  Time is close but not exact (%.0fm difference)\n", targetMinutes-totalMinutes)
				toKeep = worklogs
				toDelete = []tracker.Worklog{}
				keptMinutes = totalMinutes

				if dryRun {
					fmt.Println("[DRY RUN] Would normalize to exact target")
					return nil
				}
			} else {
				fmt.Printf("\n⚠️  DUPLICATES DETECTED - total exceeds threshold by %.1fh\n", (totalMinutes-threshold)/60)

			// Sort by start time
			sortedWorklogs := make([]tracker.Worklog, len(worklogs))
			copy(sortedWorklogs, worklogs)
			for i := 0; i < len(sortedWorklogs)-1; i++ {
				for j := i + 1; j < len(sortedWorklogs); j++ {
					if sortedWorklogs[j].Start.Time.Before(sortedWorklogs[i].Start.Time) {
						sortedWorklogs[i], sortedWorklogs[j] = sortedWorklogs[j], sortedWorklogs[i]
					}
				}
			}

			// Group by (issue_key, description)
			fmt.Println("\n  Detecting semantic duplicates (same issue + description):")
			type groupKey struct {
				issueKey    string
				description string
			}
			groups := make(map[groupKey][]tracker.Worklog)

			for _, wl := range sortedWorklogs {
				key := groupKey{
					issueKey:    wl.Issue.Key,
					description: wl.Comment,
				}
				groups[key] = append(groups[key], wl)
			}

			// Keep largest in each group
			toKeep = []tracker.Worklog{}
			toDelete = []tracker.Worklog{}
			keptMinutes = 0.0

			for key, groupWorklogs := range groups {
				if len(groupWorklogs) == 1 {
					toKeep = append(toKeep, groupWorklogs[0])
					minutes, _ := tracker.ParseISO8601Duration(groupWorklogs[0].Duration)
					keptMinutes += minutes
					fmt.Printf("  ✅ %-15s  %-40s  1 entry (%.0fm)\n",
						key.issueKey, key.description, minutes)
				} else {
					// Sort by duration descending
					for i := 0; i < len(groupWorklogs)-1; i++ {
						for j := i + 1; j < len(groupWorklogs); j++ {
							durI, _ := tracker.ParseISO8601Duration(groupWorklogs[i].Duration)
							durJ, _ := tracker.ParseISO8601Duration(groupWorklogs[j].Duration)
							if durJ > durI {
								groupWorklogs[i], groupWorklogs[j] = groupWorklogs[j], groupWorklogs[i]
							}
						}
					}

					// Keep largest
					toKeep = append(toKeep, groupWorklogs[0])
					minutes, _ := tracker.ParseISO8601Duration(groupWorklogs[0].Duration)
					keptMinutes += minutes

					fmt.Printf("  ⚠️  %-15s  %-40s  %d entries (DUPLICATES)\n",
						key.issueKey, key.description, len(groupWorklogs))
					for i, wl := range groupWorklogs {
						m, _ := tracker.ParseISO8601Duration(wl.Duration)
						if i == 0 {
							fmt.Printf("      [%d] KEEP   %.0fm (largest)\n", i+1, m)
						} else {
							fmt.Printf("      [%d] DELETE %.0fm\n", i+1, m)
							toDelete = append(toDelete, wl)
						}
					}
				}
			}

			// Overage normalization: remove largest worklogs if still over target
			if keptMinutes > targetMinutes {
				fmt.Printf("\n⚠️  Still %.1fh over target after duplicate removal\n", (keptMinutes-targetMinutes)/60)
				fmt.Println("  Normalizing to target by removing largest worklogs...")

				// Sort by duration descending
				for i := 0; i < len(toKeep)-1; i++ {
					for j := i + 1; j < len(toKeep); j++ {
						durI, _ := tracker.ParseISO8601Duration(toKeep[i].Duration)
						durJ, _ := tracker.ParseISO8601Duration(toKeep[j].Duration)
						if durJ > durI {
							toKeep[i], toKeep[j] = toKeep[j], toKeep[i]
						}
					}
				}

				finalKeep := []tracker.Worklog{}
				finalMinutes := 0.0

				for _, wl := range toKeep {
					minutes, _ := tracker.ParseISO8601Duration(wl.Duration)
					if finalMinutes+minutes <= targetMinutes {
						finalKeep = append(finalKeep, wl)
						finalMinutes += minutes
						fmt.Printf("    ✅ KEEP   %-15s  %.0fm (total: %.0fm)\n",
							wl.Issue.Key, minutes, finalMinutes)
					} else {
						toDelete = append(toDelete, wl)
						fmt.Printf("    ❌ DELETE %-15s  %.0fm (would exceed target)\n",
							wl.Issue.Key, minutes)
					}
				}

				toKeep = finalKeep
				keptMinutes = finalMinutes
			}

			fmt.Printf("\n📋 Cleanup Plan:\n")
			fmt.Printf("  Keep:    %d worklogs (%.1fh)\n", len(toKeep), keptMinutes/60)
			fmt.Printf("  Delete:  %d worklogs (%.1fh)\n", len(toDelete), (totalMinutes-keptMinutes)/60)

			if len(toDelete) > 0 {
				fmt.Println("\n  Worklogs to delete:")
				for _, wl := range toDelete {
					minutes, _ := tracker.ParseISO8601Duration(wl.Duration)
					hours := int(minutes / 60)
					mins := int(minutes) % 60
					fmt.Printf("    ❌ %-15s  %2dh %2dm  %s (ID: %s)\n",
						wl.Issue.Key, hours, mins, wl.Comment, wl.ID.String())
				}
			}

			if dryRun {
				fmt.Println("\n[DRY RUN] No worklogs were deleted")
				return nil
			}

			// Delete worklogs
			if len(toDelete) > 0 {
				fmt.Println("\n🗑️  Deleting duplicate worklogs...")
			}

			deletedCount = 0
			for _, wl := range toDelete {
				worklogID := wl.ID.String()
				err := trackerClient.DeleteWorklog(wl.Issue.Key, worklogID)
				if err != nil {
					logger.Error("Failed to delete worklog",
						zap.String("issue", wl.Issue.Key),
						zap.String("id", worklogID),
						zap.Error(err))
					fmt.Printf("  ❌ Failed to delete %s (ID: %s): %v\n", wl.Issue.Key, worklogID, err)
				} else {
					deletedCount++
					fmt.Printf("  ✅ Deleted %s (ID: %s)\n", wl.Issue.Key, worklogID)
				}
			}

			if deletedCount > 0 {
				fmt.Printf("\n✅ Cleanup completed: %d worklogs deleted\n", deletedCount)
				fmt.Printf("   Remaining time: %.1fh (%.0f minutes)\n", keptMinutes/60, keptMinutes)
			}
			}

			// Final normalization: adjust to EXACTLY target (runs for both paths)
			if !dryRun && keptMinutes != targetMinutes && len(toKeep) > 0 {
				diff := targetMinutes - keptMinutes
				fmt.Printf("\n⚙️  Normalizing to exact target (%.0fm difference)...\n", diff)

				// Find largest worklog to adjust
				largestIdx := 0
				largestMinutes := 0.0
				for i, wl := range toKeep {
					m, _ := tracker.ParseISO8601Duration(wl.Duration)
					if m > largestMinutes {
						largestMinutes = m
						largestIdx = i
					}
				}

				largest := toKeep[largestIdx]
				newMinutes := largestMinutes + diff

				if newMinutes > 0 {
					// Delete and recreate with adjusted duration
					worklogID := largest.ID.String()
					if err := trackerClient.DeleteWorklog(largest.Issue.Key, worklogID); err == nil {
						fmt.Printf("   Adjusted %-15s: %.0fm → %.0fm\n", largest.Issue.Key, largestMinutes, newMinutes)

						// Create with exact duration
						hours := int(newMinutes / 60)
						mins := int(newMinutes) % 60
						duration := fmt.Sprintf("PT%dH%dM", hours, mins)

						if _, err := trackerClient.CreateWorklog(largest.Issue.Key, largest.Start.Time, duration, largest.Comment); err == nil {
							fmt.Printf("   ✅ Normalized to exact target: %.1fh (%.0fm)\n", targetMinutes/60, targetMinutes)
						}
					}
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&dateStr, "date", "d", "", "Date to cleanup (required: today, yesterday, or YYYY-MM-DD)")
	cmd.MarkFlagRequired("date")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be done without deleting worklogs")

	return cmd
}
