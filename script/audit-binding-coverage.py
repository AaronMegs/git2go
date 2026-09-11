#!/usr/bin/env python3
# Reports per-module binding coverage of the libgit2 public API.
# Usage:  python3 script/audit-binding-coverage.py [module ...]

"""Per-module binding coverage: libgit2 public API vs symbols referenced by git2go.

Reads GIT_EXTERN declarations from each public header, then checks which of
those symbols appear anywhere in the Go/C sources of the binding.
"""
import glob
import os
import re
import sys

ROOT = os.environ.get("GIT2GO_ROOT", os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
HEADERS = sorted(glob.glob(os.path.join(ROOT, "vendor/libgit2/include/git2/*.h")))

# Build one corpus of every symbol the binding mentions.
corpus_parts = []
for pat in ("*.go", "*.c", "*.h"):
    for path in glob.glob(os.path.join(ROOT, pat)):
        if "/vendor/" in path:
            continue
        with open(path, encoding="utf-8", errors="replace") as fh:
            corpus_parts.append(fh.read())
corpus = "\n".join(corpus_parts)
# Match call sites and extern declarations, i.e. `git_foo(`. A bare mention is
# not enough: `git_odb_backend_pack_options` appears where `one_pack` reuses
# that options type, which must not count as binding `git_odb_backend_pack`.
referenced = set(re.findall(r"\b(git_[a-z0-9_]+)\s*\(", corpus))

EXTERN = re.compile(r"GIT_EXTERN\s*\([^)]*\)\s*(git_[a-z0-9_]+)")

rows = []
for header in HEADERS:
    module = os.path.basename(header)[:-2]
    with open(header, encoding="utf-8", errors="replace") as fh:
        text = fh.read()
    apis = sorted(set(EXTERN.findall(text)))
    if not apis:
        continue
    missing = [a for a in apis if a not in referenced]
    bound = len(apis) - len(missing)
    rows.append((module, bound, len(apis), missing))

rows.sort(key=lambda r: (r[1] / r[2], -r[2]))

print(f"{'module':<20} {'bound':>7}  {'pct':>4}")
print("-" * 40)
for module, bound, total, _ in rows:
    print(f"{module:<20} {bound:>3}/{total:<3} {bound*100//total:>4}%")

print("\n=== Modules with zero bindings (whole feature area absent) ===")
for module, bound, total, missing in rows:
    if bound == 0:
        print(f"  {module:<18} 0/{total}")

focus = sys.argv[1:] if len(sys.argv) > 1 else []
if focus:
    print("\n=== Unbound symbols in requested modules ===")
    for module, bound, total, missing in rows:
        if module in focus:
            print(f"\n{module} ({bound}/{total}):")
            for m in missing:
                print(f"    {m}")
