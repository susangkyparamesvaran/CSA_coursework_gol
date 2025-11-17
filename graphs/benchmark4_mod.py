import pandas as pd
import matplotlib.pyplot as plt
import numpy as np

# --- Load CSV ---
# If you're running this from the /graphs folder:
df = pd.read_csv("benchmark_neighbors_results.csv")

# --- Convert numeric columns ---
df["Threads"] = df["Threads"].astype(int)
df["Time(s)"] = df["Time(s)"].astype(float)

# --- Compute speedup relative to 1-thread baseline (per mode) ---
speedup_data = []
for mode in df["Mode"].unique():
    subset = df[df["Mode"] == mode].sort_values("Threads").copy()
    baseline = subset.loc[subset["Threads"] == 1, "Time(s)"].iloc[0]
    subset["Speedup"] = baseline / subset["Time(s)"]
    speedup_data.append(subset)

df_speedup = pd.concat(speedup_data)

# --- Prepare for side-by-side bars ---
threads = sorted(df_speedup["Threads"].unique())
bar_width = 0.35
x = np.arange(len(threads))

pre = df_speedup[df_speedup["Mode"] == "PrecomputedOffsets"]["Speedup"].values
inline = df_speedup[df_speedup["Mode"] == "InlineComputation"]["Speedup"].values

# --- Plot ---
plt.figure(figsize=(8, 5))
bars1 = plt.bar(x - bar_width/2, pre, width=bar_width, label="Precomputed Offsets", color="#4A90E2")
bars2 =plt.bar(x + bar_width/2, inline, width=bar_width, label="Inline Computation", color="#F5A623")

# --- Labels on each bar ---
for bar in bars1 + bars2:
    yval = bar.get_height()
    plt.text(bar.get_x() + bar.get_width()/2, yval + 0.03,
             f"{yval:.2f}s", ha="center", va="bottom", fontsize=8)

plt.title("Neighbour Counting Method — Speedup Comparison\n(512×512, 1000 turns, persistent workers)")
plt.xlabel("Threads")
plt.ylabel("Speedup × (vs 1-thread baseline)")
plt.xticks(x, threads)
plt.grid(True, linestyle="--", alpha=0.5, axis="y")
plt.legend()
plt.tight_layout()
plt.savefig("neighbors_speedup_bar.png", dpi=300)
plt.show()
