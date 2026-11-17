#!/usr/bin/env python3
# /// script
# requires-python = ">=3.9"
# dependencies = ["pyyaml"]
# ///
"""Diff telemetry contracts (metrics / attributes / resource attributes / events)
declared in mdatagen metadata.yaml files between two git refs.

Usage (from repo root):
  uv run --with pyyaml .claude/skills/upstream-sync/scripts/metrics_diff.py \
      --old v0.152.0 --new v0.163.0 [--only receiver/kubeletstatsreceiver ...] \
      [--focus-file components.txt] [--json out.json] > report.md

Typical runs during a sync:
  1. Upstream impact:   --old <old upstream tag> --new <new upstream tag>
  2. Fork regression:   --old main --new sync/<new tag>   (must show NO removals
                        of metrics that main had, other than ones upstream removed)

Only reads git objects; never touches the working tree.
"""
import argparse
import difflib
import json
import subprocess
import sys

import yaml


def git(*args):
    return subprocess.run(["git", *args], check=True, capture_output=True, text=True).stdout


def list_metadata(ref):
    out = git("ls-tree", "-r", "--name-only", ref)
    return [
        p for p in out.splitlines()
        if p.endswith("/metadata.yaml")
        and "/testdata/" not in p
        and not p.startswith(("cmd/", "internal/tools", ".github/"))
    ]


def load(ref, path):
    try:
        return yaml.safe_load(git("show", f"{ref}:{path}")) or {}
    except subprocess.CalledProcessError:
        return None
    except yaml.YAMLError as e:
        print(f"WARN: cannot parse {ref}:{path}: {e}", file=sys.stderr)
        return None


def attr_name(attrs, key):
    """Emitted attribute name; suffixed with ' (opt_in)' when it is not emitted by default.
    A recommended->opt_in flip therefore shows up as a removed attribute (dimension loss)."""
    a = (attrs or {}).get(key) or {}
    name = a.get("name_override") or key
    if a.get("requirement_level") == "opt_in" or a.get("enabled") is False:
        name += " (opt_in)"
    return name


def mtype(m):
    for t in ("sum", "gauge", "histogram", "exponential_histogram"):
        if t in m:
            v = m[t] or {}
            extra = ""
            if t == "sum":
                extra = f"/{'mono' if v.get('monotonic') else 'nonmono'}/{v.get('aggregation_temporality', '')}"
            return f"{t}{extra}/{v.get('value_type', '')}"
    return "?"


def deprecation(m):
    w = m.get("warnings") or {}
    st = m.get("stability") or {}
    msgs = [str(v) for v in w.values() if v]
    if isinstance(st, dict) and st.get("level") == "deprecated":
        msgs.append("stability: deprecated")
    return "; ".join(msgs)


def summarize(doc):
    attrs = doc.get("attributes") or {}
    metrics = {}
    for name, m in (doc.get("metrics") or {}).items():
        m = m or {}
        metrics[name] = {
            "enabled": bool(m.get("enabled", False)),
            "unit": m.get("unit"),
            "type": mtype(m),
            "desc": (m.get("description") or "").strip(),
            "attrs": sorted(attr_name(attrs, a) for a in (m.get("attributes") or [])),
            "deprecated": deprecation(m),
        }
    res = {}
    for name, r in (doc.get("resource_attributes") or {}).items():
        r = r or {}
        res[name] = {"enabled": bool(r.get("enabled", False))}
    events = {}
    for name, e in (doc.get("events") or {}).items():
        e = e or {}
        events[name] = {
            "enabled": bool(e.get("enabled", False)),
            "attrs": sorted(attr_name(attrs, a) for a in (e.get("attributes") or [])),
        }
    return {"type": doc.get("type"), "metrics": metrics, "resource": res, "events": events}


def similar(a, b):
    return difflib.SequenceMatcher(None, a, b).ratio()


def pair_renames(removed, added, old_m, new_m):
    """Greedy rename pairing: same description, or same type+unit with high name similarity."""
    pairs, used = [], set()
    for r in sorted(removed):
        best, score = None, 0.0
        for a in sorted(added):
            if a in used:
                continue
            o, n = old_m[r], new_m[a]
            s = similar(r, a)
            if o["desc"] and o["desc"] == n["desc"]:
                s += 1.0
            if o["type"].split("/")[0] == n["type"].split("/")[0] and o["unit"] == n["unit"]:
                s += 0.2
            if s > score:
                best, score = a, s
        if best and score >= 0.85:
            pairs.append((r, best, round(score, 2)))
            used.add(best)
    return pairs


def diff_component(path, old, new):
    out = {"path": path, "type": (new or old).get("type")}
    if old is None:
        out["status"] = "added"
        out["metrics_added"] = sorted(new["metrics"])
        return out
    if new is None:
        out["status"] = "removed"
        out["metrics_removed"] = sorted(old["metrics"])
        return out
    om, nm = old["metrics"], new["metrics"]
    removed = set(om) - set(nm)
    added = set(nm) - set(om)
    renames = pair_renames(removed, added, om, nm)
    for r, a, _ in renames:
        removed.discard(r)
        added.discard(a)
    changes = []
    for k in sorted(set(om) & set(nm)):
        o, n = om[k], nm[k]
        if o["enabled"] != n["enabled"]:
            changes.append((k, "enabled", o["enabled"], n["enabled"]))
        if o["unit"] != n["unit"]:
            changes.append((k, "unit", o["unit"], n["unit"]))
        if o["type"] != n["type"]:
            changes.append((k, "type", o["type"], n["type"]))
        if o["attrs"] != n["attrs"]:
            ra = sorted(set(o["attrs"]) - set(n["attrs"]))
            aa = sorted(set(n["attrs"]) - set(o["attrs"]))
            changes.append((k, "attributes", f"-{ra}" if ra else "", f"+{aa}" if aa else ""))
        if n["deprecated"] and not o["deprecated"]:
            changes.append((k, "newly-deprecated", "", n["deprecated"]))
    orr, nr = old["resource"], new["resource"]
    res_changes = [(k, "removed", "", "") for k in sorted(set(orr) - set(nr))]
    res_changes += [(k, "added", "", "enabled" if nr[k]["enabled"] else "disabled") for k in sorted(set(nr) - set(orr))]
    res_changes += [
        (k, "enabled", orr[k]["enabled"], nr[k]["enabled"])
        for k in sorted(set(orr) & set(nr)) if orr[k]["enabled"] != nr[k]["enabled"]
    ]
    oe, ne = old["events"], new["events"]
    ev_changes = [(k, "removed") for k in sorted(set(oe) - set(ne))] + [(k, "added") for k in sorted(set(ne) - set(oe))]
    ev_changes += [(k, "attributes changed") for k in sorted(set(oe) & set(ne)) if oe[k]["attrs"] != ne[k]["attrs"]]
    if not (removed or added or renames or changes or res_changes or ev_changes):
        return None
    out.update(
        status="changed",
        metrics_removed=sorted(removed),
        metrics_added=sorted(added),
        metrics_renamed=renames,
        metric_changes=changes,
        resource_changes=res_changes,
        event_changes=ev_changes,
        old_enabled={k: om[k]["enabled"] for k in removed | {r for r, _, _ in renames}},
    )
    return out


def breaking(d):
    if d["status"] == "removed":
        return True
    if d["status"] != "changed":
        return False
    if d["metrics_removed"] or d["metrics_renamed"]:
        return True
    for _, kind, o, n in d["metric_changes"]:
        if kind in ("unit", "type", "newly-deprecated") or (kind == "enabled" and o and not n):
            return True
        if kind == "attributes" and o:
            return True
    for _, kind, o, n in d["resource_changes"]:
        if kind == "removed" or (kind == "enabled" and o and not n):
            return True
    return any(k == "removed" for _, k in d["event_changes"])


def render(diffs, old_ref, new_ref, focus):
    L = [f"# Telemetry contract diff `{old_ref}` → `{new_ref}`", ""]
    L.append("Legend: **[on]** = enabled by default in the OLD ref (most likely used by dashboards/alerts).")
    L.append("")
    groups = [("Breaking / attention needed", [d for d in diffs if breaking(d)]),
              ("Non-breaking (additions only)", [d for d in diffs if not breaking(d)])]
    for title, items in groups:
        if focus:
            items.sort(key=lambda d: (not any(d["path"].startswith(f) for f in focus), d["path"]))
        L += [f"## {title} ({len(items)} components)", ""]
        for d in items:
            star = " ⭐" if focus and any(d["path"].startswith(f) for f in focus) else ""
            L.append(f"### `{d['type']}` — {d['path']} ({d['status']}){star}")
            if d["status"] == "removed":
                L.append(f"- component metadata removed/moved; metrics: {', '.join(d['metrics_removed']) or '—'}")
            elif d["status"] == "added":
                L.append(f"- new component; metrics: {', '.join(d['metrics_added']) or '—'}")
            else:
                oe = d.get("old_enabled", {})
                for m in d["metrics_removed"]:
                    L.append(f"- 🛑 metric REMOVED: `{m}`{' **[on]**' if oe.get(m) else ''}")
                for r, a, s in d["metrics_renamed"]:
                    L.append(f"- 🔁 metric RENAMED (heuristic, score {s}): `{r}` → `{a}`{' **[on]**' if oe.get(r) else ''}")
                for m, kind, o, n in d["metric_changes"]:
                    L.append(f"- ✏️ `{m}` {kind}: {o} → {n}")
                for r, kind, o, n in d["resource_changes"]:
                    L.append(f"- 🏷 resource attr `{r}` {kind} {o} {n}".rstrip())
                for e, kind in d["event_changes"]:
                    L.append(f"- 📜 event `{e}` {kind}")
                for m in d["metrics_added"]:
                    L.append(f"- ➕ metric added: `{m}`")
            L.append("")
    return "\n".join(L)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--old", required=True)
    ap.add_argument("--new", required=True)
    ap.add_argument("--only", nargs="*", default=[], help="path prefixes to restrict to")
    ap.add_argument("--focus-file", help="file with path prefixes to star/sort first (one per line)")
    ap.add_argument("--json")
    a = ap.parse_args()
    focus = []
    if a.focus_file:
        focus = [l.strip() for l in open(a.focus_file) if l.strip() and not l.startswith("#")]
    old_p, new_p = set(list_metadata(a.old)), set(list_metadata(a.new))
    paths = sorted(old_p | new_p)
    if a.only:
        paths = [p for p in paths if any(p.startswith(o) for o in a.only)]
    # Detect moved components (same `type`, different path) so they don't show as removed+added.
    diffs = []
    olds = {p: load(a.old, p) for p in paths if p in old_p}
    news = {p: load(a.new, p) for p in paths if p in new_p}
    olds = {p: summarize(d) for p, d in olds.items() if d is not None and (d.get("metrics") or d.get("resource_attributes") or d.get("events"))}
    news = {p: summarize(d) for p, d in news.items() if d is not None and (d.get("metrics") or d.get("resource_attributes") or d.get("events"))}
    gone = {olds[p]["type"]: p for p in set(olds) - set(news)}
    born = {news[p]["type"]: p for p in set(news) - set(olds)}
    moved = {gone[t]: born[t] for t in set(gone) & set(born) if t}
    for p in sorted(set(olds) | set(news)):
        if p in moved.values():
            continue
        if p in moved:
            d = diff_component(f"{p} → {moved[p]}", olds[p], news[moved[p]])
        else:
            d = diff_component(p, olds.get(p), news.get(p))
        if d:
            diffs.append(d)
    print(render(diffs, a.old, a.new, focus))
    if a.json:
        with open(a.json, "w") as f:
            json.dump(diffs, f, indent=2, default=str)


if __name__ == "__main__":
    main()
