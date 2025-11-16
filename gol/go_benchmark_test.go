package gol_test

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"uk.ac.bris.cs/gameoflife/gol"
)

// BenchmarkScalability generates a CSV of runtime results for multiple thread counts.
func BenchmarkScalability(b *testing.B) {
	// Ensure working directory is project root
	wd, _ := os.Getwd()
	rootPath := wd
	if strings.HasSuffix(wd, "/gol") {
		rootPath = filepath.Dir(wd)
	}
	os.Chdir(rootPath)

	// Disable stdout printing from program (optional)
	os.Stdout = nil

	// Parameters
	imageW, imageH := 512, 512
	turns := 1000
	threadCounts := []int{1, 2, 4, 8, 16}

	// Prepare CSV file
	csvFile, err := os.Create("benchmark_results.csv")
	if err != nil {
		b.Fatalf("failed to create csv: %v", err)
	}
	defer csvFile.Close()

	writer := csv.NewWriter(csvFile)
	defer writer.Flush()

	// Header row
	writer.Write([]string{"Threads", "Time(s)", "Speedup", "Efficiency"})

	fmt.Printf("\n%-10s %-12s %-10s %-10s\n", "Threads", "Time(s)", "Speedup", "Eff.")

	var serialTime float64

	for _, threads := range threadCounts {
		p := gol.Params{
			Turns:           turns,
			Threads:         threads,
			ImageWidth:      imageW,
			ImageHeight:     imageH,
			Dynamic:         false,
			ChunksPerWorker: 1,
			UsePrecomputed:  true,
		}

		start := time.Now()
		events := make(chan gol.Event)
		go gol.Run(p, events, nil)
		for range events {
		}
		elapsed := time.Since(start).Seconds()

		var speedup, efficiency float64
		if threads == 1 {
			serialTime = elapsed
			speedup, efficiency = 1, 1
		} else {
			speedup = serialTime / elapsed
			efficiency = speedup / float64(threads)
		}

		// Write to console
		fmt.Printf("%-10d %-12.3f %-10.2fx %-10.2f\n", threads, elapsed, speedup, efficiency)

		// Write to CSV
		writer.Write([]string{
			fmt.Sprint(threads),
			fmt.Sprintf("%.3f", elapsed),
			fmt.Sprintf("%.2f", speedup),
			fmt.Sprintf("%.2f", efficiency),
		})
	}

	fmt.Println("\n Results saved to benchmark_results.csv")
}

func BenchmarkDynamic(b *testing.B) {
	// Ensure working directory is project root
	wd, _ := os.Getwd()
	rootPath := wd
	if strings.HasSuffix(wd, "/gol") {
		rootPath = filepath.Dir(wd)
	}
	os.Chdir(rootPath)

	// Disable stdout printing from program (optional)
	os.Stdout = nil

	// Parameters
	imageW, imageH := 512, 512
	turns := 1000
	threadCounts := []int{1, 2, 4, 8, 16}

	// Prepare CSV file
	csvFile, err := os.Create("benchmark_dynamic_results.csv")
	if err != nil {
		b.Fatalf("failed to create csv: %v", err)
	}
	defer csvFile.Close()

	writer := csv.NewWriter(csvFile)
	defer writer.Flush()

	// Add header row
	writer.Write([]string{"Mode", "Threads", "Time(s)"})

	modes := []struct {
		name    string
		dynamic bool
	}{
		{"Persistent", false},
		{"Dynamic", true},
	}

	for _, mode := range modes {
		fmt.Printf("\n--- %s Workers ---\n", mode.name)
		for _, threads := range threadCounts {
			p := gol.Params{
				Turns:           turns,
				Threads:         threads,
				ImageWidth:      imageW,
				ImageHeight:     imageH,
				Dynamic:         mode.dynamic,
				ChunksPerWorker: 1, //default
				UsePrecomputed:  true,
			}

			start := time.Now()
			events := make(chan gol.Event)
			go gol.Run(p, events, nil)
			for range events {
			}
			elapsed := time.Since(start).Seconds()

			// Console feedback
			fmt.Printf("%-10s %2d threads -> %.3fs\n", mode.name, threads, elapsed)

			// Write to CSV
			writer.Write([]string{
				mode.name,
				fmt.Sprint(threads),
				fmt.Sprintf("%.3f", elapsed),
			})
		}
	}

	fmt.Println("\n Results saved to benchmark_dynamic_results.csv")
}

func BenchmarkGranularity(b *testing.B) {
	// Ensure project root
	wd, _ := os.Getwd()
	rootPath := wd
	if strings.HasSuffix(wd, "/gol") {
		rootPath = filepath.Dir(wd)
	}
	os.Chdir(rootPath)

	os.Stdout = nil

	imageW, imageH := 512, 512
	turns := 1000
	threadCounts := []int{1, 2, 4, 8, 16}
	chunkFactors := []int{1, 2, 4, 8, 16}

	csvFile, err := os.Create("benchmark_granularity_results.csv")
	if err != nil {
		b.Fatalf("failed to create CSV: %v", err)
	}
	defer csvFile.Close()

	writer := csv.NewWriter(csvFile)
	defer writer.Flush()
	writer.Write([]string{"Threads", "ChunksPerWorker", "Time(s)"})

	for _, threads := range threadCounts {
		for _, chunks := range chunkFactors {
			p := gol.Params{
				Turns:           turns,
				Threads:         threads,
				ImageWidth:      imageW,
				ImageHeight:     imageH,
				Dynamic:         false, //
				ChunksPerWorker: chunks,
				UsePrecomputed:  true,
			}

			start := time.Now()
			fmt.Printf("→ Running threads=%d chunks=%d...\n", threads, chunks)
			events := make(chan gol.Event)
			go gol.Run(p, events, nil)
			for range events {
			}

			fmt.Printf("✓ Done threads=%d chunks=%d\n", threads, chunks)
			elapsed := time.Since(start).Seconds()

			writer.Write([]string{
				fmt.Sprint(threads),
				fmt.Sprint(chunks),
				fmt.Sprintf("%.3f", elapsed),
			})

			fmt.Printf("Threads=%d, Chunks=%d → %.3fs\n", threads, chunks, elapsed)
		}
	}
}

func BenchmarkNeighbourCounting(b *testing.B) {
	// Ensure working directory is project root
	wd, _ := os.Getwd()
	rootPath := wd
	if strings.HasSuffix(wd, "/gol") {
		rootPath = filepath.Dir(wd)
	}
	os.Chdir(rootPath)

	os.Stdout = nil

	// Experiment parameters
	imageW, imageH := 512, 512
	turns := 1000
	threadCounts := []int{1, 2, 4, 8, 16}

	// Prepare CSV file
	csvFile, err := os.Create("benchmark_neighbors_results.csv")
	if err != nil {
		b.Fatalf("failed to create CSV: %v", err)
	}
	defer csvFile.Close()

	writer := csv.NewWriter(csvFile)
	defer writer.Flush()
	writer.Write([]string{"Mode", "Threads", "Time(s)"})

	// Two variants of neighbour calculation
	modes := []struct {
		name       string
		precompute bool
	}{
		{"PrecomputedOffsets", true},
		{"InlineComputation", false},
	}

	for _, mode := range modes {
		fmt.Printf("\n--- %s ---\n", mode.name)
		for _, threads := range threadCounts {
			p := gol.Params{
				Turns:           turns,
				Threads:         threads,
				ImageWidth:      imageW,
				ImageHeight:     imageH,
				Dynamic:         false, // persistent model
				ChunksPerWorker: 1,
				UsePrecomputed:  false,
			}

			start := time.Now()
			events := make(chan gol.Event)
			go gol.Run(p, events, nil)
			for range events {
			}
			elapsed := time.Since(start).Seconds()

			writer.Write([]string{
				mode.name,
				fmt.Sprint(threads),
				fmt.Sprintf("%.3f", elapsed),
			})
			fmt.Printf("%-18s Threads=%-2d → %.3fs\n", mode.name, threads, elapsed)
		}
	}
}

func BenchmarkOptimized(b *testing.B) {
	wd, _ := os.Getwd()
	rootPath := wd
	if strings.HasSuffix(wd, "/gol") {
		rootPath = filepath.Dir(wd)
	}
	os.Chdir(rootPath)

	os.Stdout = nil

	csvFile, err := os.Create("benchmark_optimized_results.csv")
	if err != nil {
		b.Fatalf("failed to create CSV: %v", err)
	}
	defer csvFile.Close()

	writer := csv.NewWriter(csvFile)
	defer writer.Flush()

	writer.Write([]string{"Threads", "Chunks", "UsePrecomputed", "Time(s)"})

	imageW, imageH := 512, 512
	turns := 1000

	// --- SERIAL + BASELINE PARALLEL ---
	baselineThreadCounts := []int{1, 2, 4, 8, 16}
	for _, t := range baselineThreadCounts {
		p := gol.Params{
			Turns:           turns,
			Threads:         t,
			ImageWidth:      imageW,
			ImageHeight:     imageH,
			Dynamic:         false,
			ChunksPerWorker: 1,    // default
			UsePrecomputed:  true, // serial + baseline both use this
		}

		start := time.Now()
		events := make(chan gol.Event)
		go gol.Run(p, events, nil)
		for range events {
		}
		elapsed := time.Since(start).Seconds()

		writer.Write([]string{
			fmt.Sprint(t),
			"1",
			"true",
			fmt.Sprintf("%.3f", elapsed),
		})
	}

	// --- OPTIMIZED PARALLEL ---
	optimizedThreads := []int{8, 16}
	optimizedChunks := []int{2, 4}

	for _, t := range optimizedThreads {
		for _, c := range optimizedChunks {
			p := gol.Params{
				Turns:           turns,
				Threads:         t,
				ImageWidth:      imageW,
				ImageHeight:     imageH,
				Dynamic:         false,
				ChunksPerWorker: c,
				UsePrecomputed:  false, // inline version
			}

			start := time.Now()
			events := make(chan gol.Event)
			go gol.Run(p, events, nil)
			for range events {
			}
			elapsed := time.Since(start).Seconds()

			writer.Write([]string{
				fmt.Sprint(t),
				fmt.Sprint(c),
				"false",
				fmt.Sprintf("%.3f", elapsed),
			})
		}
	}
}
