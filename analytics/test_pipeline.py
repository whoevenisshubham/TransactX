import copy,json,hashlib,tempfile,unittest
from pathlib import Path
import numpy as np
from sklearn.ensemble import IsolationForest
import pipeline
from check_fixtures import check
A=pipeline.ROOT/'analytics/artifacts'
class PipelineChecks(unittest.TestCase):
 def setUp(self):self.rows=json.loads((A/'dataset.json').read_text())
 def test_schema_and_fixture_hashes(self):check();pipeline.validate(self.rows)
 def test_run_leak_rejected(self):
  rows=copy.deepcopy(self.rows);rows[-1]['runId']=rows[0]['runId']
  with self.assertRaises(AssertionError):pipeline.validate(rows)
 def test_future_time_rejected(self):
  rows=copy.deepcopy(self.rows);rows[-1]['from']='2026-09-01T00:00:00Z';rows[-1]['to']='2026-09-01T00:05:00Z'
  with self.assertRaises(AssertionError):pipeline.validate(rows)
 def test_label_feature_rejected(self):
  rows=copy.deepcopy(self.rows);rows[0]['features']['scenario']=1
  with self.assertRaises(Exception):pipeline.validate(rows)
 def test_metadata_does_not_enter_features(self):
  before=pipeline.matrix(self.rows);rows=copy.deepcopy(self.rows)
  for r in rows:r['anomaly']=not r['anomaly'];r['scenario']='changed';r['runId']='changed';r['futureOutcome']='FAILED'
  np.testing.assert_array_equal(before,pipeline.matrix(rows))
 def test_analytics_contract_fixtures(self):
  from jsonschema import Draft202012Validator,FormatChecker
  schema=json.loads((pipeline.ROOT/'docs/next-gen/execution-pack/docs/next-gen/contracts/v2.schema.json').read_text())
  v=Draft202012Validator({'$ref':'#/$defs/AnalyticsWindow','$defs':schema['$defs']},format_checker=FormatChecker())
  for window in json.loads((A/'windows.json').read_text()):v.validate(window)
 def test_projection_queue_scenario(self):
  runs={}
  for row in self.rows:runs.setdefault(row['runId'],[]).append(row)
  for rows in runs.values():
   pending=0
   for row in sorted(rows,key=lambda r:r['from']):
    pending+=row['sampleCount']-row['projectionAppliedEvents']
    self.assertEqual(pending,row['projectionPendingEvents'])
    self.assertGreaterEqual(pending,0)
    if row['scenario']=='projection_backlog' and row['anomaly']:self.assertGreater(pending,0)
    else:self.assertEqual(pending,0)
  before=pipeline.matrix(self.rows);changed=copy.deepcopy(self.rows)
  for row in changed:row['projectionPendingEvents']=999999;row['projectionAppliedEvents']=999999
  np.testing.assert_array_equal(before,pipeline.matrix(changed))
 def test_artifact_hashes(self):
  manifest=json.loads((A/'manifest.json').read_text())
  for name,sha in manifest['files'].items():self.assertEqual(sha,pipeline.digest(A/name))
 def test_reproducible_training_export(self):
  with tempfile.TemporaryDirectory() as tmp:
   out=Path(tmp);pipeline.run(A/'dataset.json',out,'2026-10-11T00:00:00Z')
   for name in ['model.json','evaluation.json','reference.json','split.json','baseline.json','windows.json']:self.assertEqual((A/name).read_bytes(),(out/name).read_bytes(),name)
 def test_estimator_feature_mapping_export(self):
  x=pipeline.matrix([r for r in self.rows if r['split']=='train' and not r['anomaly']])
  forest=IsolationForest(n_estimators=2,max_features=.5,max_samples=32,random_state=371).fit(x.astype(np.float32))
  a=pipeline.export(forest,np.zeros(6),np.ones(6),.6,self.rows,'test','test','2026-10-11T00:00:00Z')
  for exported,estimator,mapping in zip(a['trees'],forest.estimators_,forest.estimators_features_):
   for node,f in zip(exported['nodes'],estimator.tree_.feature):self.assertEqual(node['feature'],int(mapping[f]) if f>=0 else -2)
if __name__=='__main__':unittest.main()
