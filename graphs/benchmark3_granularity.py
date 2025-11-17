import pandas as pd
import matplotlib.pyplot as plt

# --- Load CSV ---
df = pd.read_csv("benchmark_granularity_results.csv")

# --- Ensure numeric types and sort properly ---
df["Threads"] = df["Threads"].astype(int)
df["ChunksPerWorker"] = df["ChunksPerWorker"].astype(int)
df["Time(s)"] = df["Time(s)"].astype(float)
df = df.sort_values(["Threads", "ChunksPerWorker"])

# --- Compute per-thread baseline (1 chunk per worker) ---
speedup_data = []
for threads, subset in df.groupby("Threads"):
    subset = subset.sort_values("ChunksPerWorker").copy()
    baseline_time = subset.loc[subset["ChunksPerWorker"] == 1, "Time(s)"].iloc[0]
    subset["Speedup"] = baseline_time / subset["Time(s)"]
    speedup_data.append(subset)

df_speedup = pd.concat(speedup_data, ignore_index=True)

# --- Sanity print ---
print(df_speedup[df_speedup["Threads"] == 16][["ChunksPerWorker", "Time(s)", "Speedup"]])

# --- Plot ---
plt.figure(figsize=(7, 5))
for threads in sorted(df_speedup["Threads"].unique()):
    subset = df_speedup[df_speedup["Threads"] == threads]
    plt.plot(subset["ChunksPerWorker"], subset["Speedup"], "o-", label=f"{threads} threads")

plt.title("Granularity Speedup (Baseline = same-thread 1-chunk)\n(512×512, 1000 turns)")
plt.xlabel("Chunks per Worker")
plt.ylabel("Speedup × (vs same-thread 1-chunk)")
plt.grid(True, linestyle="--", alpha=0.5)
plt.legend(title="Threads", loc="lower right")

# Remove tight y-limit to see full shape
plt.ylim(df_speedup["Speedup"].min() * 0.95, df_speedup["Speedup"].max() * 1.05)
plt.tight_layout()
plt.savefig("granularity_speedup_final_fixed.png", dpi=300)
plt.show()
