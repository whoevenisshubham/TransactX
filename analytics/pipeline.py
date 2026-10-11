"""Reproducible offline C5 training/evaluation/export; no pickle, network or online service."""
import argparse,hashlib,json,platform,uuid
from pathlib import Path
import numpy as np
import sklearn
from sklearn.ensemble import IsolationForest
from sklearn.metrics import precision_recall_fscore_support,average_precision_score
ROOT=Path(__file__).resolve().parents[1]
FIELDS=json.loads((ROOT/'analytics/feature_schema.json').read_text())['orderedFields']
FLOORS=np.array([.03,.03,30,1000,.2,.03])
SEED=371

def dump(path,value):
 path.write_text(json.dumps(value,indent=2,allow_nan=False)+'\n',encoding='utf-8',newline='\n')
def digest(path):return hashlib.sha256(path.read_bytes()).hexdigest()
def matrix(rows):
 # Explicit allowlist: labels, IDs, scenario and all provenance stay out of inference.
 return np.array([[r['features'][f] for f in FIELDS] for r in rows],dtype=np.float64)
def validate(rows):
 from jsonschema import Draft202012Validator
 schema=json.loads((ROOT/'analytics/feature_schema.json').read_text())['schema']
 v=Draft202012Validator(schema);run_splits={};times={k:[] for k in ['train','validation','test']};seen=set()
 for r in rows:
  assert r['synthetic'] is True and r['featureSchemaVersion']=='tx.features.v1'
  assert r['sampleCount']>=20 and r['from']<r['to']
  v.validate(r['features']);assert np.isfinite(list(r['features'].values())).all()
  assert set(r['features'])==set(FIELDS)
  assert run_splits.setdefault(r['runId'],r['split'])==r['split']
  key=(r['runId'],r['from']);assert key not in seen;seen.add(key)
  times[r['split']].append((r['from'],r['to']))
 assert len(run_splits)>=30
 assert max(t[1] for t in times['train'])<min(t[0] for t in times['validation'])
 assert max(t[1] for t in times['validation'])<min(t[0] for t in times['test'])
 return {k:sorted(r for r,s in run_splits.items() if s==k) for k in times}
def metrics(y,scores,threshold):
 pred=scores>threshold
 p,r,f,_=precision_recall_fscore_support(y,pred,average='binary',zero_division=0)
 return dict(precision=float(p),recall=float(r),f1=float(f),prAuc=float(average_precision_score(y,scores)),falseAlertsPerHealthyWindow=float(pred[~y].mean()),threshold=float(threshold))
def select(y,scores,candidates):
 # Fixed validation policy: maximize F1 subject to <=5% false alerts, tie -> stricter threshold.
 results=[metrics(y,scores,t) for t in candidates]
 eligible=[m for m in results if m['falseAlertsPerHealthyWindow']<=.05]
 assert eligible
 return max(eligible,key=lambda m:(m['f1'],m['threshold'])),results

def export(forest,center,scale,threshold,rows,split_hash,data_hash,trained_at):
 trees=[]
 for estimator,mapping in zip(forest.estimators_,forest.estimators_features_):
  t=estimator.tree_;nodes=[]
  for i in range(t.node_count):
   f=int(t.feature[i]);nodes.append(dict(feature=int(mapping[f]) if f>=0 else -2,threshold=float(t.threshold[i]),left=int(t.children_left[i]),right=int(t.children_right[i]),samples=int(t.n_node_samples[i])))
  assert len(nodes)<=1024
  trees.append(dict(nodes=nodes))
 # Safety envelope is frozen from training calibration only, NOT fitted on validation/test anomalies.
 # 128 robust IQRs is a broad engineering guard, not learned proof of in-distribution behavior.
 return dict(formatVersion='tx.iforest.v1',featureSchemaVersion='tx.features.v1',version='synthetic-iforest-v2',trainedAt=trained_at,sourceCommit=rows[0]['sourceCommit'],datasetSha256=data_hash,splitSha256=split_hash,synthetic=True,features=FIELDS,center=center.tolist(),scale=scale.tolist(),lower=[-128.]*6,upper=[128.]*6,samples=int(forest.max_samples_),threshold=threshold,trees=trees)

def run(dataset,out,trained_at):
 rows=json.loads(dataset.read_text());split=validate(rows);out.mkdir(parents=True,exist_ok=True)
 dump(out/'split.json',split);split_hash=digest(out/'split.json');data_hash=digest(dataset)
 train=[r for r in rows if r['split']=='train' and not r['anomaly']]
 val=[r for r in rows if r['split']=='validation'];test=[r for r in rows if r['split']=='test']
 x=matrix(train);center=np.median(x,axis=0);scale=np.percentile(x,75,axis=0)-np.percentile(x,25,axis=0);scale[scale==0]=1
 forest=IsolationForest(n_estimators=64,max_samples=min(256,len(train)),max_features=1.,bootstrap=False,random_state=SEED,n_jobs=1).fit(((x-center)/scale).astype(np.float32))
 def score(rs):return -forest.score_samples(((matrix(rs)-center)/scale).astype(np.float32))
 base_center=np.median(x,axis=0);base_scale=np.maximum(1.4826*np.median(np.abs(x-base_center),axis=0),FLOORS)
 def base(rs):
  d=(matrix(rs)-base_center)/base_scale;d[:,5]*=-1
  return np.maximum(d.max(axis=1),0)
 vy=np.array([r['anomaly'] for r in val]);ty=np.array([r['anomaly'] for r in test])
 chosen,comparisons=select(vy,score(val),np.linspace(.45,.8,71));bc,bcomparisons=select(vy,base(val),[2.,3.,4.,5.,6.,8.,10.])
 artifact=export(forest,center,scale,chosen['threshold'],rows,split_hash,data_hash,trained_at)
 dump(out/'model.json',artifact);assert (out/'model.json').stat().st_size<=5*1024*1024
 ms,bs=score(test),base(test)
 z=(matrix(test)-center)/scale;ready=((z>=artifact['lower'])&(z<=artifact['upper'])).all(axis=1)
 hybrid=np.where(ready,ms>chosen['threshold'],bs>bc['threshold'])
 delay={}
 for scenario in sorted(set(r['scenario'] for r in test)):
  delay[scenario]={}
  for name,scores,threshold in [('model',ms,chosen['threshold']),('baseline',bs,bc['threshold'])]:
   values=[]
   for run_id in sorted(set(r['runId'] for r in test if r['scenario']==scenario)):
    inds=[i for i,r in enumerate(test) if r['runId']==run_id and r['anomaly']]
    if not inds:continue
    detected=next((j*5 for j,i in enumerate(inds) if scores[i]>threshold),None);values.append(detected)
   delay[scenario][name+'DelayMinutes']=values
 rng=np.random.default_rng(SEED);runs=sorted(set(r['runId'] for r in test));boot={'model':[],'baseline':[]}
 for _ in range(500):
  selected=rng.choice(runs,size=len(runs),replace=True);inds=[i for rid in selected for i,r in enumerate(test) if r['runId']==rid]
  for name,scores,threshold in [('model',ms,chosen['threshold']),('baseline',bs,bc['threshold'])]:boot[name].append(metrics(ty[inds],scores[inds],threshold)['f1'])
 report=dict(synthetic=True,sourceCommit=rows[0]['sourceCommit'],datasetSha256=data_hash,splitSha256=split_hash,modelSha256=digest(out/'model.json'),seed=SEED,rows=len(rows),runs=len(set(r['runId'] for r in rows)),trainingHealthyRows=len(train),splitRunCounts={k:len(v) for k,v in split.items()},model=metrics(ty,ms,chosen['threshold']),baseline=metrics(ty,bs,bc['threshold']),validationCandidates={'model':comparisons,'baseline':bcomparisons},detectionDelay=delay,scenarioBootstrapF1Interval={k:np.quantile(v,[.025,.975]).tolist() for k,v in boot.items()},bootstrapReplicates=500,dependencies={'python':platform.python_version(),'numpy':np.__version__,'scikit-learn':sklearn.__version__},limitations=['Virtual-time simulated adapter observations; no live HTTP, database or production training data.','Simulated projection queue depth and traffic burst volume stay outside the frozen six features.','Only two held-out runs per scenario; bootstrap uncertainty is coarse.','128-IQR guard is an engineering envelope, not learned drift calibration.'])
 report['modelReadyWindows']=int(ready.sum());report['modelOutOfDistributionWindows']=int((~ready).sum())
 report['fallbackPolicy']='Use baseline when model is out_of_distribution; recommend baseline-only because model underperforms.'
 report['hybrid']=metrics(ty,hybrid.astype(float),.5)
 report['modelMetricsScope']='Raw sklearn model scores before native distribution guard; hybrid includes guard/fallback.'
 dump(out/'evaluation.json',report)
 # >=100 sklearn reference scores for held-out normal and anomalous windows.
 # A separate Go regression covers float32 tree-threshold branching.
 refs=[]
 for r,s,is_ready in zip(test,ms,ready):refs.append(dict(status='ready' if is_ready else 'out_of_distribution',vector=dict(schemaVersion='tx.features.v1',sampleCount=r['sampleCount'],values=[r['features'][f] for f in FIELDS]),score=float(s),baselineDeviation=float(base([r])[0]),baselineAlert=bool(base([r])[0]>bc['threshold'])))
 dump(out/'reference.json',refs)
 dump(out/'baseline.json',dict(threshold=bc['threshold'],warmup=8,center=base_center.tolist(),scale=base_scale.tolist(),calibrationRunIds=split['train']))
 windows=[]
 for i in [0,8,40,56]:
  row=test[i];ref=refs[i]
  windows.append(dict(id=str(uuid.uuid5(uuid.NAMESPACE_URL,'tx-c5-window-'+row['runId']+row['from'])),entityType='execution_target',entityId='RAIL-A',**{'from':row['from'],'to':row['to']},sampleCount=row['sampleCount'],featureSchemaVersion='tx.features.v1',features=row['features'],baselineAlert={'code':'operational_deviation','message':'Observed feature deviation exceeds baseline threshold.'} if ref['baselineAlert'] else None,model=dict(version=artifact['version'],status=ref['status'],score=ref['score'] if ref['status']=='ready' else None,threshold=artifact['threshold'] if ref['status']=='ready' else None),advisory=True))
 dump(out/'windows.json',windows)
 dump(out/'manifest.json',dict(version='tx.c5.artifacts.v1',files={p.name:digest(p) for p in sorted(out.glob('*.json')) if p.name!='manifest.json'}))
 print(json.dumps({k:report[k] for k in ['rows','runs','trainingHealthyRows','model','baseline','modelSha256']},indent=2))
if __name__=='__main__':
 parser=argparse.ArgumentParser();parser.add_argument('--dataset',type=Path,default=ROOT/'analytics/artifacts/dataset.json');parser.add_argument('--out',type=Path,default=ROOT/'analytics/artifacts');parser.add_argument('--trained-at',default='2026-10-11T00:00:00Z');args=parser.parse_args();run(args.dataset,args.out,args.trained_at)
