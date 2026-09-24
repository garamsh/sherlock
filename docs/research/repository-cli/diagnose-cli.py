"""Free CLI resource-shim probes; writes synthetic output under .local only."""
import os,subprocess,json,pathlib
root=pathlib.Path(__file__).resolve().parents[3]/'.local/verification/github-840-comparison'
root.mkdir(parents=True, exist_ok=True)
rev='a0cbffbd144fbaa95699d930bcb23f590f4186c4'
env={k:v for k,v in os.environ.items() if k not in ('GH_TOKEN','GITHUB_TOKEN','MINIMAX_KEY')}
rows=[]
for setting in ('default','GOMAXPROCS=2','MALLOC_ARENA_MAX=2'):
 for attempt in range(5):
  cmd=['docker','run','--rm','--name','gagent-cli-840-diagnosis','--user','10001','--cap-drop','ALL','-e','GH_HOST=host.docker.internal','-e','GH_ENTERPRISE_TOKEN=synthetic-local-only']
  if setting!='default':cmd+=['-e',setting]
  cmd+=['--entrypoint','/gagent','gagent-workspace:'+rev,'exec-limits','cpu=60','as=2147483648','fsize=104857600','nofile=1024','nproc=64','--','/usr/bin/gh','gh','repo','clone','fixture/repo','/tmp/fixture']
  r=subprocess.run(cmd,env=env,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=30)
  (root/f'diagnose-{setting.split("=")[0]}-{attempt}.log').write_bytes(r.stdout)
  rows.append({'setting':setting,'attempt':attempt,'exit':r.returncode,'pthread_failure':b'pthread_create' in r.stdout,'output_bytes':len(r.stdout)})
(root/'cli-diagnosis.json').write_text(json.dumps({'method':'direct built-image resource shim, UID 10001, identical runtime caps, synthetic enterprise host token; no model or target repository requests','rows':rows},indent=2))
print(json.dumps(rows))
