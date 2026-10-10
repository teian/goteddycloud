#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Measure a binary through real loopback HTTP using fresh synthetic data."""
import argparse, hashlib, json, os, pathlib, re, subprocess, tempfile, time
workspace=pathlib.Path(__file__).resolve().parents[1]
parser=argparse.ArgumentParser()
parser.add_argument('--binary',type=pathlib.Path,required=True)
parser.add_argument('--probe',type=pathlib.Path,required=True)
args=parser.parse_args()
binary,probe=args.binary.resolve(),args.probe.resolve()
with tempfile.TemporaryDirectory(prefix='goteddycloud-benchmark-') as temporary:
 root=pathlib.Path(temporary)/'fixture'
 subprocess.run(['go','run','-buildvcs=false','./cmd/fixturegen','-output',str(root)],cwd=workspace,check=True,capture_output=True)
 with (pathlib.Path(temporary)/'server.log').open('w') as log:
  process=subprocess.Popen([str(binary),'-b',str(root),'--listen','127.0.0.1:0'],stdout=subprocess.DEVNULL,stderr=log,env=dict(os.environ,GOMAXPROCS='4'))
  try:
   deadline=time.monotonic()+10
   while True:
    match=re.search(r'http:(127\.0\.0\.1:\d+)',(pathlib.Path(temporary)/'server.log').read_text())
    if match:break
    if process.poll() is not None or time.monotonic()>deadline:raise RuntimeError('benchmark server did not become ready')
    time.sleep(.02)
   output=subprocess.check_output([str(probe),'-url','http://'+match[1]+'/v1/time'],env=dict(os.environ,GOMAXPROCS='4'))
   print(json.dumps({'binary_sha256':hashlib.sha256(binary.read_bytes()).hexdigest(),'concurrency':4,'gomaxprocs':4,'rounds':json.loads(output)},indent=2))
  finally:
   process.terminate()
   try:process.wait(timeout=5)
   except subprocess.TimeoutExpired:process.kill();process.wait();raise RuntimeError('benchmark server required forced termination')
