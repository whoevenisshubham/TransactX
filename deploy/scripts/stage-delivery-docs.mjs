import { readFile, realpath, mkdir, writeFile } from 'node:fs/promises';
import { resolve, relative, isAbsolute, dirname } from 'node:path';

const [repository, destination] = process.argv.slice(2);
if (!repository) throw new Error('Repository path is required.');
const root = await realpath(repository);
const sources = [
  'docs/architecture.md', 'docs/next-gen/DELIVERY_ARCHITECTURE.md', 'docs/next-gen/DELIVERY_REPORT.md',
  'docs/next-gen/state/M1_INTEGRATION_REQUIREMENTS.md',
  'docs/next-gen/state/candidate902-acceptance-m1.json',
  'docs/next-gen/state/final-completion-acceptance.json',
  'docs/next-gen/state/production-browser-final-m1.json',
  'docs/next-gen/state/native-crash-restore-final-m1.json',
  'docs/next-gen/state/cross-bank-native-recovery-m1.json',
  'docs/next-gen/state/https-smoke-platform-m1.json',
  'deploy/README.md', 'deploy/RESTORE_REHEARSAL.md',
  'deploy/verification/native-financial-proof.sql',
  'deploy/verification/cross-bank-financial-proof.sql',
  'docs/runbooks/google-sign-in.md',
  'docs/runbooks/gemini-zero-spend.md',
  'docs/runbooks/demo-enrichment.md',
];
const files = [];
for (const source of sources) {
  const path = await realpath(resolve(root, source));
  const within = relative(root, path);
  if (within.startsWith('..') || isAbsolute(within)) throw new Error(`Delivery input escapes repository: ${source}`);
  const text = (await readFile(path, 'utf8')).replaceAll('\r\n', '\n');
  if (!text.trim()) throw new Error(`Empty delivery input: ${source}`);
  if (source.endsWith('.json')) JSON.parse(text);
  files.push([source, text]);
}
if (destination) {
  const stage = resolve(destination);
  if (stage === root) throw new Error('Delivery staging must not overwrite repository inputs.');
  for (const [source, text] of files) {
    const target = resolve(stage, source);
    await mkdir(dirname(target), { recursive: true });
    await writeFile(target, text, 'utf8');
  }
}
process.stdout.write(`Validated ${files.length} delivery documents/evidence/queries${destination ? ' and staged with LF encoding' : ''}.\n`);
