"""Purpose-break only the remote working copy; always restore exact bytes."""
from pathlib import Path
import subprocess
root=Path(__file__).resolve().parents[3]
mutations=[
 ('result','internal/deploy/publish.go','normalizeUpload(answer)','result(false, "failed", "mutated", 0)','TestPublishBoundary'),
 ('journal-submitted','internal/deploy/publish.go','Submitted: ptr(true)','Submitted: ptr(false)','TestSubmittedBeforeNetworkAndLostResponse'),
 ('request','internal/deploy/publish.go','strconv.FormatBool(o.Activate)','strconv.FormatBool(false)','TestSubmittedBeforeNetworkAndLostResponse'),
 ('applied-state','internal/agents/deployments.go','if bound && jsonx.Equal','if false && bound && jsonx.Equal','TestPublishBoundary'),
 ('late-receipt','internal/agents/deployments.go','if options.UploadReceipt &&','if false && options.UploadReceipt &&','TestLateUploadCannotRegressPoll'),
 ('agent-journal','internal/agents/agentfake/agentfake.go','State{Digest: sum, Status:','State{Digest: "mutated", Status:','TestPublishBoundary'),
]
for name,relative,before,after,test in mutations:
 path=root/relative;original=path.read_bytes();text=original.decode();assert text.count(before)==1,(name,text.count(before))
 try:
  path.write_text(text.replace(before,after,1))
  result=subprocess.run(['go','test','-count=1','-run','^'+test+'$','./internal/deploy'],cwd=root,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True)
  assert result.returncode!=0,(name,'mutation escaped')
  assert 'build failed' not in result.stdout,(name,'compile failure is not a killed mutation')
  print(name+': detected',flush=True)
 finally:path.write_bytes(original)
print('A14 mutation gate: 6/6 detected; original source restored')
