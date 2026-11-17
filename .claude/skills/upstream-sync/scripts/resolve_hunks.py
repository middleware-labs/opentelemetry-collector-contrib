#!/usr/bin/env python3
"""Resolve git conflict hunks in a file, hunk by hunk, with explicit choices.

Usage:
  resolve_hunks.py FILE --show            # print numbered hunks (ours=fork, theirs=upstream)
  resolve_hunks.py FILE CHOICE [CHOICE..] # one choice per hunk, in order

Choices:
  o   keep ours (fork side, HEAD)
  t   keep theirs (upstream tag)
  ot  ours then theirs (union; fork lines first)
  to  theirs then ours (union; upstream lines first)
  Ot  ours minus its LAST line, then theirs (both sides re-declare the same trailing
      key, e.g. `metric.x:` vs `metric.x: # (old)`; keep upstream's spelling)
  Tt  theirs minus its LAST line, then ours... (rare; mirror of Ot)

Refuses to write if the number of choices != number of hunks.
Supports diff3 style (|||||||) by discarding the base section.
"""
import sys


def parse(lines):
    out, hunks, i = [], [], 0
    while i < len(lines):
        if lines[i].startswith("<<<<<<< "):
            ours, base, theirs, mode = [], [], [], "o"
            i += 1
            while not lines[i].startswith(">>>>>>> "):
                l = lines[i]
                if l.startswith("||||||| "):
                    mode = "b"
                elif l.startswith("=======") and l.rstrip("\n") == "=======":
                    mode = "t"
                else:
                    {"o": ours, "b": base, "t": theirs}[mode].append(l)
                i += 1
            hunks.append((ours, theirs))
            out.append(len(hunks) - 1)
        else:
            out.append(lines[i])
        i += 1
    return out, hunks


def main():
    path, args = sys.argv[1], sys.argv[2:]
    lines = open(path).readlines()
    skel, hunks = parse(lines)
    if args == ["--show"]:
        for n, (o, t) in enumerate(hunks):
            print(f"===== hunk {n}  OURS(fork):")
            print("".join(o), end="")
            print("----- THEIRS(upstream):")
            print("".join(t), end="")
        return
    if len(args) != len(hunks):
        sys.exit(f"{path}: {len(hunks)} hunks but {len(args)} choices")
    res = []
    for item in skel:
        if isinstance(item, int):
            o, t = hunks[item]
            c = args[item]
            res += {"o": o, "t": t, "ot": o + t, "to": t + o, "Ot": o[:-1] + t, "Tt": t[:-1] + o}[c]
        else:
            res.append(item)
    open(path, "w").writelines(res)
    print(f"{path}: resolved {len(hunks)} hunks")


if __name__ == "__main__":
    main()
