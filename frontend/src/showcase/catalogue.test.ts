import { test } from "node:test";
import assert from "node:assert/strict";
import { showcaseLibrary, scenarioTime } from "./catalogue";

test("illustrative catalogue is bounded, versioned and financially coherent", () => {
  assert.equal(showcaseLibrary.source,"illustrative");
  assert.equal(showcaseLibrary.schemaVersion,1);
  assert.equal(showcaseLibrary.stories.length,8);
  const all=showcaseLibrary.stories.flatMap(story=>story.records);
  assert.equal(all.length,192);
  assert.equal(new Set(all.map(record=>record.payment.id)).size,all.length);
  for (const story of showcaseLibrary.stories) {
    assert.equal(story.source,"illustrative");
    assert.equal(story.schemaVersion,1);
    for (const {payment,timeline} of story.records) {
      assert.ok(Number.isSafeInteger(payment.amountPaise) && payment.amountPaise>0);
      assert.equal(payment.currency,"INR");
      assert.match(payment.id,/^[a-f0-9-]{36}$/);
      assert.ok(payment.createdAt.startsWith(showcaseLibrary.scenarioDate));
      assert.ok(timeline.length>=4 && timeline.length<=8);
      const times=timeline.map(event=>Date.parse(event.at));
      assert.deepEqual(times,[...times].sort((a,b)=>a-b));
      assert.equal(new Set(timeline.map(event=>event.id)).size,timeline.length);
      if(payment.state==="COMPLETED") {
        assert.ok(payment.completedAt);
        assert.ok(Date.parse(payment.completedAt)>=Date.parse(payment.createdAt));
        assert.ok(timeline.some(event=>event.at===payment.completedAt && event.source==="Coordinator"));
      } else assert.equal(payment.completedAt,undefined);
    }
  }
});

test("unknown outcomes and missing evidence never become illustrative failures",()=>{
  for (const id of ["lost-reply","missing-evidence"]) {
    const story=showcaseLibrary.stories.find(item=>item.id===id)!;
    assert.ok(story.records.every(record=>record.payment.state==="PENDING_RECONCILIATION"));
    assert.ok(story.records.every(record=>!record.payment.failureReason));
  }
  assert.equal(scenarioTime("2026-09-20T12:34:56Z"),"12:34:56");
});

test("offline intent time precedes actual central payment acceptance",()=>{
  const story=showcaseLibrary.stories.find(item=>item.id==="offline-replay")!;
  for(const record of story.records) {
    const accepted=record.timeline.find(event=>event.title==="Original request key accepted")!;
    assert.equal(record.payment.createdAt,accepted.at);
    assert.ok(Date.parse(record.timeline[0].at)<Date.parse(record.payment.createdAt));
  }
});
