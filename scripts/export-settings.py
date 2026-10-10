#!/usr/bin/env python3
"""Extract only public schema/default metadata from the synthetic pinned oracle."""
import json
import urllib.request
from pathlib import Path
url='http://127.0.0.1:18080/api/settings/getIndex?internal=true&nolevel=true'
data=json.load(urllib.request.urlopen(url,timeout=5))['options']
allowed={'ID','shortname','description','label','internal','level','type','valueInit','min','max'}
# NEVER copy current `value`: that field can hold loaded keys or user credentials.
schema=[{k:v for k,v in item.items() if k in allowed} for item in data]
# Develop hides private rows even with nolevel/internal; retain schema only.
existing=json.loads(Path('internal/config/schema.json').read_text())
public={row['ID']:row for row in schema}
schema=[public.pop(row['ID'],row) for row in existing if row['ID'] in public or row.get('level')==99]+list(public.values())
assert len(schema)>200
p=Path('internal/config/schema.json')
p.write_text(json.dumps(schema,indent=2)+'\n')
print('Exported',len(schema),'definitions; excluded all current values')
