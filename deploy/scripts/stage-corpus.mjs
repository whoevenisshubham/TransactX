import { readFile, realpath, mkdir, writeFile } from 'node:fs/promises';
import { resolve, dirname, relative, isAbsolute } from 'node:path';
import { createHash } from 'node:crypto';

const [repository, destination] = process.argv.slice(2);
if (!repository) throw new Error('Repository path is required.');
const root = await realpath(repository);
const manifestPath = 'docs/runbooks/corpus-manifest.json';
const manifestText = (await readFile(resolve(root, manifestPath), 'utf8')).replaceAll('\r\n', '\n');
const manifest = JSON.parse(manifestText);
if (!Array.isArray(manifest.documents) || manifest.documents.length === 0) throw new Error('Approved corpus documents are required.');
const files = [[manifestPath, manifestText]];
for (const document of manifest.documents) {
  if (typeof document.path !== 'string' || !/^docs\/[a-zA-Z0-9_./-]+\.md$/.test(document.path) || document.path.split('/').includes('..')) {
    throw new Error('Corpus document path must stay within docs.');
  }
  const source = await realpath(resolve(root, document.path));
  const within = relative(root, source);
  if (within.startsWith('..') || isAbsolute(within)) throw new Error('Corpus source escapes repository.');
  const content = (await readFile(source, 'utf8')).replaceAll('\r\n', '\n');
  const hash = createHash('sha256').update(content).digest('hex');
  if (hash !== document.sha256) throw new Error(`Approved corpus hash mismatch: ${document.path}`);
  files.push([document.path, content]);
}
if (destination) {
  for (const [path, content] of files) {
    const output = resolve(destination, path);
    await mkdir(dirname(output), { recursive: true });
    await writeFile(output, content, 'utf8');
  }
}
console.log(`Approved corpus inputs verified: ${manifest.documents.length} documents${destination ? ' staged' : ''}.`);
