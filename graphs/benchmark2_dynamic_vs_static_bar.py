import pandas as pd
import matplotlib.pyplot as plt
import numpy as np


df = pd.read_csv("benchmark_dynamic_results.csv")

# Split by mode
persistent = df[df["Mode"] == "Persistent"]
dynamic = df[df["Mode"] == "Dynamic"]

threads = persistent["Threads"]
x = np.arange(len(threads))
width = 0.35  # bar width

plt.figure(figsize=(7, 4.5))

# Bars
bars1 = plt.bar(x - width/2, persistent["Time(s)"], width,
                label="Persistent Workers", color="#4A90E2")
bars2 = plt.bar(x + width/2, dynamic["Time(s)"], width,
                label="Dynamic Workers", color="#F5A623")

# Labels on each bar
for bar in bars1 + bars2:
    yval = bar.get_height()
    plt.text(bar.get_x() + bar.get_width()/2, yval + 0.03,
             f"{yval:.2f}s", ha="center", va="bottom", fontsize=8)

# Axes and title
plt.title("Game of Life – Persistent vs Dynamic Worker Creation\n(512×512, 1000 turns)")
plt.xlabel("Threads")
plt.ylabel("Runtime (s)")
plt.xticks(x, threads)
plt.ylim(0, max(df["Time(s)"]) * 1.25)
plt.grid(axis="y", linestyle="--", alpha=0.5)
plt.legend()

plt.tight_layout()
plt.savefig("worker_lifetime_comparison.png", dpi=300)
plt.show()
