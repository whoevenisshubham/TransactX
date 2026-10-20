"""Validate C0 evidence and C4 UI fixtures against the unchanged shared schema."""
import hashlib,json,os
from pathlib import Path
from jsonschema import Draft202012Validator,FormatChecker
ROOT=Path(__file__).resolve().parents[1]
schema=json.loads((ROOT/'docs/api/contracts/v2.schema.json').read_text())
def check():
 fixtures=ROOT/'backend/internal/investigator/testfixtures'
 manifest=json.loads((fixtures/'manifest.json').read_text())
 assert manifest['schemaSha256']==hashlib.sha256((ROOT/'docs/api/contracts/v2.schema.json').read_bytes().replace(b'\r\n',b'\n')).hexdigest()
 for entry in manifest['cases']:
  raw=(fixtures/entry['path']).read_bytes();assert hashlib.sha256(raw).hexdigest()==entry['sha256']
  c=json.loads(raw)
  for key,definition in [('evidence','EvidenceBundle'),('report','InvestigationReport')]:
   Draft202012Validator({'$ref':'#/$defs/'+definition,'$defs':schema['$defs']},format_checker=FormatChecker()).validate(c[key])
  e=dict(c['evidence']);snapshot=e.pop('snapshotId')
  assert snapshot=='sha256:'+hashlib.sha256(json.dumps(e,sort_keys=True,separators=(',',':'),ensure_ascii=False).encode()).hexdigest()
  forbidden={'fullName','note','token','idempotencyKey','password','email'}
  def scan(x):
   if isinstance(x,dict):
    assert not forbidden.intersection(x)
    for v in x.values():scan(v)
   elif isinstance(x,list):
    for v in x:scan(v)
  scan(c['evidence'])
 evaluation_path=Path(os.environ.get('TRANSACTX_EVALUATION_PATH',ROOT/'backend/internal/investigator/evaluation/results.json'))
 evaluation=json.loads(evaluation_path.read_text())
 investigation_validator=Draft202012Validator({'$ref':'#/$defs/Investigation','$defs':schema['$defs']},format_checker=FormatChecker())
 for name,fixture in evaluation['uiFixtures'].items():
  investigation_validator.validate(fixture)
  scan(fixture)
 assert evaluation['metrics']['providerCalls']==0
 assert evaluation['metrics']['paidCostUsd']=='0.00'
 assert evaluation['liveProvider']['status']=='pending'
 print(f"C0/C4: {len(manifest['cases'])} immutable evidence fixtures and {len(evaluation['uiFixtures'])} investigation UI fixtures valid")
if __name__=='__main__':check()
