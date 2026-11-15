"""Check statement-weighted backend coverage by architecture layer."""
import json
from pathlib import Path
import sys

MINIMUM = {"domain": 90, "application": 80, "infrastructure": 60, "global": 80}
counts = {layer: [0, 0] for layer in MINIMUM}
lines = Path(sys.argv[1]).read_text(encoding="utf-8").splitlines()
if not lines or not lines[0].startswith("mode: "):
    sys.exit("invalid Go coverage profile")
blocks = {}
for line in lines[1:]:
    location, statements, count = line.rsplit(" ", 2)
    statements = int(statements)
    if location in blocks and blocks[location][0] != statements:
        sys.exit("inconsistent statement count in coverage profile")
    previous = blocks.get(location, (statements, False))[1]
    blocks[location] = (statements, previous or int(count) > 0)
for location, (statements, covered) in blocks.items():
    layers = ["global"]
    for folder, layer in (("domain", "domain"), ("application", "application"), ("adapter", "infrastructure")):
        if f"/internal/{folder}/" in location:
            layers.append(layer)
    for layer in layers:
        counts[layer][0] += statements if covered else 0
        counts[layer][1] += statements
result = {layer: {"covered": covered, "statements": total,
                  "percentage": round(100 * covered / total, 2) if total else 0,
                  "minimum": MINIMUM[layer], "passed": total > 0 and 100 * covered >= MINIMUM[layer] * total}
          for layer, (covered, total) in counts.items()}
print(json.dumps(result, indent=2))
sys.exit(0 if all(layer["passed"] for layer in result.values()) else 1)
