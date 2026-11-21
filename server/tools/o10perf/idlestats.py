"""Idle window statistics from an o10perf idle-series.json.

Usage: python3 idlestats.py <run-dir> [...]
Prints, per server: average CPU, 10 s window median / p10 / p90 / max (single-core %),
the same after the first 5 minutes, and the Go allocation rate (window median).
"""
import json
import os
import statistics
import sys

TICK = os.sysconf('SC_CLK_TCK')


def pct(values, q):
    values = sorted(values)
    return values[min(len(values) - 1, int(q * len(values)))]


def windows(series):
    out = []
    for a, b in zip(series, series[1:]):
        dt = b['t'] - a['t']
        cpu = (b['ticks'] - a['ticks']) / TICK / dt * 100
        alloc = None
        if 'go' in a and 'go' in b:
            alloc = (b['go']['total_alloc'] - a['go']['total_alloc']) / dt / 1e6
        out.append((b['t'], cpu, alloc))
    return out


def main():
    for run in sys.argv[1:]:
        data = json.load(open(os.path.join(run, 'idle-series.json')))
        for name, series in data.items():
            w = windows(series)
            span = series[-1]['t'] - series[0]['t']
            avg = (series[-1]['ticks'] - series[0]['ticks']) / TICK / span * 100
            cpus = [c for _, c, _ in w]
            late = [c for t, c, _ in w if t > 300]
            allocs = [a for t, _, a in w if a is not None and t > 300]
            line = (f'{run} {name}: avg {avg:.2f}% median {statistics.median(cpus):.2f}% p10 {pct(cpus, .1):.2f}% '
                    f'p90 {pct(cpus, .9):.2f}% max {max(cpus):.1f}% | after 5 min avg {statistics.mean(late):.2f}% '
                    f'median {statistics.median(late):.2f}%')
            if allocs:
                line += f' | alloc median {statistics.median(allocs):.2f} MB/s'
            print(line)


if __name__ == '__main__':
    main()
