import pandas as pd
import matplotlib.pyplot as plt


df = pd.read_csv("benchmark_optimized_results.csv")

# Normalize values
df["UsePrecomputed"] = df["UsePrecomputed"].astype(str).str.strip().str.lower()
df["Threads"] = df["Threads"].astype(int)
df["Chunks"] = df["Chunks"].astype(int)
df["Time(s)"] = df["Time(s)"].astype(float)

print("Unique UsePrecomputed values:", df["UsePrecomputed"].unique())

# Extract serial time
serial_time = df[
    (df["Threads"] == 1) &
    (df["Chunks"] == 1) &
    (df["UsePrecomputed"] == "true")
]["Time(s)"].iloc[0]

# Extract best baseline parallel (precomputed, 1 chunk)
baseline_time = df[
    (df["Chunks"] == 1) &
    (df["Threads"] > 1) &
    (df["UsePrecomputed"] == "true")
]["Time(s)"].min()

# Extract best optimized parallel (inline, multi-chunk) 
optimized_time = df[
    (df["UsePrecomputed"] == "false")
]["Time(s)"].min()

print("\nTimes:")
print("Serial:", serial_time)
print("Baseline:", baseline_time)
print("Optimized:", optimized_time)

# Speedups 
baseline_speedup = serial_time / baseline_time
optimized_speedup = serial_time / optimized_time

labels = ["Serial", "Baseline Parallel", "Optimized Parallel"]
speedups = [1, baseline_speedup, optimized_speedup]
times = [serial_time, baseline_time, optimized_time]


# Plot
plt.figure(figsize=(8, 5))
bars = plt.bar(labels, speedups, color=["#f4a261", "#4da3ff", "#7ce36f"], width=0.45)
plt.ylim(0, max(speedups) * 1.2)


# Annotate bars
for bar, speedup, t in zip(bars, speedups, times):
    height = bar.get_height()
    plt.text(
        bar.get_x() + bar.get_width() / 2,
        height + 0.05,
        f"{speedup:.2f}×\n({t:.3f}s)",
        ha="center",
        fontsize=11,
    )

plt.title("Final Comparison: Serial vs Parallel Implementations")
plt.ylabel("Speedup × (vs Serial)")
plt.grid(axis="y", linestyle="--", alpha=0.4)
plt.tight_layout()
plt.savefig("final_comparison.png", dpi=300)
plt.show()
