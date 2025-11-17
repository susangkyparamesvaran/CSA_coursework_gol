import pandas as pd
import matplotlib.pyplot as plt

# --- Load CSV ---
df = pd.read_csv("benchmark_optimized_results.csv")

# Normalize values
df["UsePrecomputed"] = df["UsePrecomputed"].astype(str).str.strip().str.lower()
df["Threads"] = df["Threads"].astype(int)
df["Chunks"] = df["Chunks"].astype(int)
df["Time(s)"] = df["Time(s)"].astype(float)

# --- Extract serial ---
serial_row = df[
    (df["Threads"] == 1) &
    (df["Chunks"] == 1) &
    (df["UsePrecomputed"] == "true")
].iloc[0]
serial_time = serial_row["Time(s)"]

# --- Extract best baseline parallel ---
baseline_row = df[
    (df["Chunks"] == 1) &
    (df["Threads"] > 1) &
    (df["UsePrecomputed"] == "true")
].sort_values("Time(s)").iloc[0]
baseline_time = baseline_row["Time(s)"]

# --- Extract best optimised parallel ---
optimized_row = df[
    (df["UsePrecomputed"] == "false")
].sort_values("Time(s)").iloc[0]
optimized_time = optimized_row["Time(s)"]

# --- Compute speedups ---
baseline_speedup = serial_time / baseline_time
optimized_speedup = serial_time / optimized_time

# X-axis: ordered categories
labels = ["Serial", "Baseline Parallel", "Optimized Parallel"]
x = [0, 1, 2]

speedups = [1, baseline_speedup, optimized_speedup]
times = [serial_time, baseline_time, optimized_time]
configs = [
    f"{serial_row['Threads']} threads, {serial_row['Chunks']} chunks",
    f"{baseline_row['Threads']} threads, {baseline_row['Chunks']} chunks",
    f"{optimized_row['Threads']} threads, {optimized_row['Chunks']} chunks"
]

# --- Plot ---
plt.figure(figsize=(10, 6))

# Plot line + points
plt.plot(x, speedups, marker="o", markersize=10, linewidth=2.5, color="#4A90E2")

# Above-point labels (speedup + time)
for xi, s, t in zip(x, speedups, times):
    plt.text(
        xi,
        s + 0.12 * max(speedups),
        f"{s:.2f}×\n({t:.3f}s)",
        ha="center",
        fontsize=12,
    )

# Below x-axis tick labels: threads + chunks
for xi, cfg in zip(x, configs):
    plt.text(
        xi, -0.07,
        cfg,
        ha="center",
        va="top",
        fontsize=11,
        transform=plt.gca().get_xaxis_transform()
    )

plt.subplots_adjust(bottom=0.22)


# Formatting
plt.xticks(x, labels)
plt.ylabel("Speedup × (vs Serial)")
plt.title("Final Comparison: Serial vs Parallel Implementations")
plt.grid(axis="y", linestyle="--", alpha=0.4)
plt.ylim(-0.5, max(speedups) * 1.4)
plt.tight_layout()

plt.savefig("final_comparison_line.png", dpi=300)
plt.show()
