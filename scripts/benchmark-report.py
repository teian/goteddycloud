#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Compare medians from repeated Go benchmarks; no statistical claims implied."""
import argparse, json, pathlib, re, statistics
parser=argparse.ArgumentParser()
parser.add_argument('before',type=pathlib.Path)
parser.add_argument('after',type=pathlib.Path)
parser.add_argument('--http-before',type=pathlib.Path)
parser.add_argument('--http-after',type=pathlib.Path)
args=parser.parse_args()
pattern=re.compile(r'^(Benchmark\S+)-\d+\s+\d+\s+([\d.]+) ns/op\s+(\d+) B/op\s+(\d+) allocs/op')
def medians(path):
 samples={}
 for line in path.read_text().splitlines():
  match=pattern.match(line)
  if match:samples.setdefault(match[1],[]).append(tuple(map(float,match.groups()[1:])))
 if not samples:raise ValueError('no benchmark samples in '+str(path))
 return {name:tuple(statistics.median(row[index] for row in rows) for index in range(3)) for name,rows in samples.items()}
before,after=medians(args.before),medians(args.after)
if set(before)!=set(after):raise ValueError('benchmark sets differ')
print('| Benchmark | Before µs/op | After µs/op | Speedup | Bytes/op before → after | Allocs/op before → after |')
print('| --- | ---: | ---: | ---: | ---: | ---: |')
for name,old in before.items():
 new=after[name]
 print(f'| {name.removeprefix("Benchmark")} | {old[0]/1000:.3f} | {new[0]/1000:.3f} | {old[0]/new[0]:.1f}× | {old[1]:.0f} → {new[1]:.0f} | {old[2]:.0f} → {new[2]:.0f} |')
if args.http_before and args.http_after:
 print('\n| Real HTTP metric | Before | After |\n| --- | ---: | ---: |')
 reports=[json.loads(path.read_text())['rounds'] for path in [args.http_before,args.http_after]]
 for field,label in [('requests_per_second','Requests/sec'),('median_us','Median latency (µs)'),('p95_us','p95 latency (µs)')]:
  values=[statistics.median(row[field] for row in report) for report in reports]
  print(f'| {label} | {values[0]:,.1f} | {values[1]:,.1f} |')
