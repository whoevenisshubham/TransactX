import { useEffect, useState } from "react";
import { Amount, Badge, Icon, PageHeader, StatusBadge } from "../components";
import { PublicHeader } from "../public/LandingPage";
import { showcaseLibrary, scenarioTime } from "./catalogue";
import "./showcase.css";

export function ShowcasePage() {
  // Enter a fresh illustrative context; never inherit live IDs from a URL.
  useEffect(() => { window.history.replaceState({}, "", "/showcase"); }, []);
  const [storyIndex, setStoryIndex] = useState(0);
  const [recordIndex, setRecordIndex] = useState(0);
  const [page, setPage] = useState(0);
  const story = showcaseLibrary.stories[storyIndex];
  const record = story.records[recordIndex];
  const payment = record.payment;
  const pageSize = 8;
  const records = story.records.slice(page*pageSize,(page+1)*pageSize);
  const reference = `EXAMPLE ${String(recordIndex+1).padStart(3,"0")}`;
  function selectStory(index: number) { setStoryIndex(index); setRecordIndex(0); setPage(0); }
  return <div className="public-site showcase-site reset-public reset-library"><a className="skip-link" href="#main">Skip to content</a><PublicHeader />
    <div className="showcase-source"><div className="public-container"><strong><Icon name="activity" />Illustrative scenario — not the live network</strong><span>Fixed scenario date: {showcaseLibrary.scenarioDate} · UTC</span></div></div>
    <main id="main" className="public-container showcase-main">
      <PageHeader eyebrow="The scenario library" title="Follow the difficult moments." description="Eight guided stories about payment reliability. Explore the evidence, and the limits of what it can tell you." />
      <div className="showcase-intro"><p>192 synthetic records · Read-only examples · No account required</p><a className="text-link" href="/demo">Open the live demo <Icon name="arrow" /></a></div>
      <div className="showcase-layout"><nav className="showcase-stories" aria-label="Illustrative stories">{showcaseLibrary.stories.map((item,index)=><button key={item.id} type="button" aria-pressed={index===storyIndex} onClick={()=>selectStory(index)}><span className="showcase-number">{String(index+1).padStart(2,"0")}</span><span><small>{item.category}</small><strong>{item.title}</strong></span><Icon name="arrow" size={16} /></button>)}</nav>
        <div className="showcase-detail" key={story.id}><section className="showcase-story-heading"><Badge tone="accent">Story {storyIndex+1} / 8</Badge><h2>{story.title}</h2><p>{story.summary}</p></section>
          <div className="showcase-boundary"><Icon name="alert" /><p>{story.boundary}</p></div>
          <section className="showcase-payment" aria-labelledby="example-payment-title"><div className="section-heading"><div><p className="eyebrow">Selected example · {reference}</p><h3 id="example-payment-title">Asha → Example Studio</h3></div><StatusBadge state={payment.state} /></div><Amount paise={payment.amountPaise} prominent /><p className="showcase-bank-path">{payment.sourceBankName}<Icon name="arrow" size={16}/>{payment.destinationBankName}</p><p className="showcase-record-time">Scenario payment created at {scenarioTime(payment.createdAt)} UTC · {showcaseLibrary.scenarioDate}</p></section>
          <section className="showcase-evidence" aria-labelledby="example-evidence-title"><div className="section-heading"><h3 id="example-evidence-title">The evidence trail</h3><span>Scripted observations</span></div><ol>{record.timeline.map((event,index)=><li key={event.id} id={`example-evidence-${index+1}`} tabIndex={-1}><span className="showcase-event-number">{index+1}</span><div><div className="showcase-event-meta"><strong>{event.source}</strong><time dateTime={event.at}>{scenarioTime(event.at)} UTC</time></div><h4>{event.title}</h4><p>{event.detail}</p></div></li>)}</ol></section>
          <section className="showcase-explanation"><p className="eyebrow">Example investigation</p><h3>What can we conclude?</h3><Badge tone="neutral">Prewritten example · Not a Gemini response</Badge><p>{story.answer}</p><nav aria-label="Example evidence citations">{record.timeline.map((event,index)=><a key={event.id} href={`#example-evidence-${index+1}`} onClick={()=>document.getElementById(`example-evidence-${index+1}`)?.focus()}>{event.source} [{index+1}]</a>)}</nav></section>
          <section className="showcase-records" aria-labelledby="example-records-title"><div className="section-heading"><div><p className="eyebrow">Explore this story</p><h3 id="example-records-title">24 fixed example records</h3></div><span>Page {page+1} of 3</span></div><p>Amounts and timestamps vary deterministically; the explanatory sequence is shared. These records never enter live metrics or model calibration.</p><div className="showcase-record-scroll" role="region" aria-label="Example record table" tabIndex={0}><table><caption>Illustrative records for {story.title}; all times UTC on {showcaseLibrary.scenarioDate}</caption><thead><tr><th scope="col">Example</th><th scope="col">Created (UTC)</th><th scope="col">Amount</th><th scope="col">Central state</th></tr></thead><tbody>{records.map((item,index)=>{const absolute=page*pageSize+index;return <tr key={item.payment.id} className={recordIndex===absolute?"is-selected":""}><td><button type="button" aria-pressed={recordIndex===absolute} aria-label={`Inspect example ${absolute+1}`} onClick={()=>setRecordIndex(absolute)}>{String(absolute+1).padStart(3,"0")}<Icon name="arrow" size={14}/></button></td><td><time dateTime={item.payment.createdAt}>{scenarioTime(item.payment.createdAt)}</time></td><td><Amount paise={item.payment.amountPaise}/></td><td><code>{item.payment.state}</code></td></tr>})}</tbody></table></div><div className="showcase-pagination"><button type="button" className="button button-secondary" disabled={page===0} onClick={()=>setPage(value=>value-1)}>Previous examples</button><button type="button" className="button button-secondary" disabled={(page+1)*pageSize>=story.records.length} onClick={()=>setPage(value=>value+1)}>Next examples</button></div></section>
        </div></div>
      <footer className="showcase-footer"><p>Scenario library v{showcaseLibrary.schemaVersion} · Source: illustrative · Fixed {showcaseLibrary.scenarioDate}</p><p>These stories demonstrate UI and domain concepts. They are not crash-test results, cryptographic proofs, live observations or provider evaluations.</p><a className="text-link" href="/demo">Continue in the authenticated demo <Icon name="arrow" /></a></footer>
    </main></div>;
}
