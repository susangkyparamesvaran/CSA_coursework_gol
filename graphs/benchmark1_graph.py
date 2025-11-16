import pandas as pd
import matplotlib.pyplot as plt

# Load data
df = pd.read_csv("benchmark_results.csv")

# Create plot
plt.figure(figsize=(6, 4))
plt.plot(df["Threads"], df["Speedup"], "o-", color="#4A90E2", label="Measured Speedup")

# Styling
plt.title("Game of Life Scalability (512×512, 1000 turns)")
plt.xlabel("Threads")
plt.ylabel("Speedup ×")

# Expand Y-axis around your data range
plt.ylim(min(df["Speedup"]) * 0.9, max(df["Speedup"]) * 1.1)

# Add data labels for clarity
for x, y in zip(df["Threads"], df["Speedup"]):
    plt.text(x, y + 0.03, f"{y:.2f}×", ha="center", va="bottom", fontsize=9)

plt.grid(True, linestyle="--", alpha=0.5)
plt.legend()
plt.tight_layout()
plt.savefig("speedup_zoomed.png", dpi=200)
plt.show()
