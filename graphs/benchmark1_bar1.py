import pandas as pd
import matplotlib.pyplot as plt
import numpy as np

df = pd.read_csv("benchmark_results.csv")
threads = df["Threads"]
speedup = df["Speedup"]
efficiency = df["Efficiency"]

x = np.arange(len(threads))
width = 0.35

fig, ax1 = plt.subplots(figsize=(6,4))

# Bars for speedup
bars1 = ax1.bar(x - width/2, speedup, width, label='Speedup ×', color="#4a90e2")
ax1.set_ylabel("Speedup ×", color="#4a90e2")
ax1.set_xlabel("Threads")
ax1.set_xticks(x)
ax1.set_xticklabels(threads)
ax1.set_title("Parallel Performance – Speedup & Efficiency")

# Second y-axis for efficiency
ax2 = ax1.twinx()
bars2 = ax2.bar(x + width/2, efficiency, width, label='Efficiency ×', color="#7ed321")
ax2.set_ylabel("Efficiency ×", color="#7ed321")

# Add grid + labels
ax1.grid(axis="y", linestyle="--", alpha=0.4)
fig.legend(loc="upper right", bbox_to_anchor=(1,1), bbox_transform=ax1.transAxes)

plt.tight_layout()
plt.savefig("speedup_efficiency_bar.png", dpi=200)
plt.show()
