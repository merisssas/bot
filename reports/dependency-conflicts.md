# Dependency conflict check

Command executed:

```bash
go mod graph | python - <<'PY'
import sys
from collections import defaultdict
versions=defaultdict(set)
for line in sys.stdin:
    parts=line.strip().split()
    if len(parts)!=2:
        continue
    mod=parts[1]
    if '@' in mod:
        name, ver = mod.split('@',1)
    else:
        name, ver = mod, ''
    versions[name].add(ver)
conflicts={k:sorted(v) for k,v in versions.items() if len(v)>1}
if not conflicts:
    print('NO_CONFLICTS')
else:
    for k,v in sorted(conflicts.items()):
        print(k, ','.join(v))
PY
```

Output:

```
NO_CONFLICTS
```
