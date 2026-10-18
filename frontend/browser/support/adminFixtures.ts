import type { Page } from "@playwright/test";
import { fulfillFixture, installFixtureApi } from "./opsFixtures";

export const participant = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
export const runId = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb";
export const operationId = "cccccccc-cccc-4ccc-8ccc-cccccccccccc";
export const start = "2026-10-11T08:00:00Z";
export const end = "2026-10-11T09:00:00Z";

export async function adminFixtures(page: Page, onChild: (participantId: string | null) => void) {
  await installFixtureApi(page, "OPS_ADMIN");
  const sampled = new Set<string>();
  await page.route("http://localhost:8080/**", async route => {
    const request = route.request();
    const url = new URL(request.url());
    const path = url.pathname;
    if (request.method() === "OPTIONS") return route.fallback();
    const circuit = (id: string) => ({ executionTargetId:id, state:id === "RAIL-A" ? "CLOSED" : "OPEN", openedAt:start, halfOpenedAt:null, failureCount:1, timeoutCount:1, consecutiveSuccesses:0, activeProbes:0, successfulProbes:0, restorationStep:0, maxRestorationSteps:3, restorationProgress:0, lastEvaluatedAt:end });
    const ref = { ParticipantID:participant, ScopeID:"browser-fixture", Generation:"fixture-generation", Path:"root", Region:{Start:start,End:end} };
    const run = { id:runId, participantId:participant, status:"COMPLETED", scopeFrom:start, scopeTo:end, startedAt:start, recordCount:2, discrepancyCount:0, canonicalRoot:"fixture-root", participantRoot:"fixture-root", canonicalVersion:"fixture.v1", algorithmVersion:"fixture.v1", elapsedNs:2_000_000, nodesVisited:3, recordsInspected:2, bytesExamined:384, divergentBuckets:0, divergentRecords:0 };
    let data: unknown;
    if (path === "/health") data = {status:"ok",service:"browser-fixture"};
    else if (path === "/health/db") return route.fulfill({ status:503, headers:{"access-control-allow-origin":"http://127.0.0.1:4173"}, contentType:"application/json", body:JSON.stringify({requestId:"fixture",error:{code:"UNAVAILABLE",message:"Fixture unavailable observation"}}) });
    else if (path === "/api/ops/circuit") data = {"RAIL-A":circuit("RAIL-A"),"RAIL-B":circuit("RAIL-B")};
    else if (/^\/api\/ops\/circuit\/[^/]+\/events$/.test(path)) data = [{id:1,executionTargetId:"RAIL-B",previousState:"CLOSED",newState:"OPEN",reason:"FIXTURE_TIMEOUT",transitionedAt:start,failureCount:1,timeoutCount:1,consecutiveSuccesses:0,activeProbes:0,successfulProbes:0,restorationStep:0,cooldownDurationMs:30000,rollingWindowMs:60000,eventType:"CIRCUIT_OPENED"}];
    else if (path === "/api/ops/routing/distribution") data = {windowHours:24,items:[{targetId:"RAIL-A",payments:2},{targetId:"RAIL-B",payments:1}]};
    else if (/^\/api\/ops\/health\/[^/]+\/sample$/.test(path)) { const target=path.split("/")[4]; sampled.add(target); data={TargetID:target,SampledAt:end,Available:true,Latency:2_000_000,Outcome:"SUCCESS",CorrelationID:"browser-fixture-probe"}; }
    else if (/^\/api\/ops\/health\/[^/]+$/.test(path)) { const target=path.split("/")[4]; data={targetId:target,score:1,availabilityScore:1,successScore:1,latencyPenalty:0,timeoutPenalty:0,sampleCount:target === "RAIL-B" ? sampled.has(target) ? 1 : 0 : 24,windowStartedAt:start,computedAt:end}; }
    else if (path === "/api/ops/chaos/scenarios") data = [];
    else if (path === "/api/ops/reconciliation/runs") data = {items:[run]};
    else if (path === `/api/ops/reconciliation/runs/${runId}/discrepancies`) data = {items:[]};
    else if (path === "/api/ops/reconciliation/tree/root") data = {rootHex:"a".repeat(64),algorithm:"browser-fixture",version:"fixture.v1",ref,region:ref.Region};
    else if (path === "/api/ops/reconciliation/tree/children") { onChild(url.searchParams.get("participantId")); data=[{hashHex:"a".repeat(64),ref:{...ref,Path:"root/0"},region:ref.Region}]; }
    else if (path === "/api/ops/integrity/status") data = {runs:[]};
    else if (path === `/api/ops/reconciliation/proof/${operationId}`) data = {operationId,record:{source:"browser-fixture"},leafHashHex:"b".repeat(64),bucketId:{partition:"fixture",start,widthNs:3600000000000},participantId:participant,scopeFrom:start,scopeTo:end,generation:"fixture-generation",canonicalVersion:"fixture.v1",algorithmVersion:"fixture.v1",bucketRootHex:"a".repeat(64),expectedRootHex:"a".repeat(64),bucketPath:[],globalPath:[],generationMetrics:{durationNs:2_000_000,bucketSiblingCount:0,globalSiblingCount:0,totalSiblingCount:0,serializedSizeBytes:128}};
    else if (path === "/api/ops/reconciliation/proof/verify") data = {valid:false,reconstructedRoot:"b".repeat(64)};
    else if (path === "/api/ops/activity") data = {items:[{id:"fixture-event",category:"CIRCUIT",eventType:"CIRCUIT_OPENED",severity:"WARNING",occurredAt:end,targetId:"RAIL-B",title:"Browser fixture circuit observation",summary:"Illustrative browser fixture; not a live observation.",details:{source:"browser-fixture"}}],limit:50};
    else return route.fallback();
    return fulfillFixture(route,data);
  });
}
