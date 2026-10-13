import assert from 'node:assert/strict';
import test from 'node:test';
import { parseConfig } from '../src/domain/config.js';
import { parseArtifact, parseReview } from '../src/domain/contracts.js';
import { artifact } from './fixture.js';

await test('configuration requires explicit verification and rejects unsupported keys', () => {
  const source = '[agent]\nprovider="openai"\nmodel="any"\n[verify]\ncommands=["make test"]\n';
  assert.deepEqual(parseConfig(source).verify, ['make test']);
  assert.equal(parseConfig(source).label, 'romp:v2');
  assert.equal(parseConfig(source).claimedLabel, 'romp:v2:claimed');
  assert.throws(
    () => parseConfig(source.replace('commands=["make test"]', 'commands=[]')),
    /explicit commands/,
  );
  assert.throws(() => parseConfig(source + '[harness]\ndefault="codex"'), /Unknown/);
  assert.throws(() => parseConfig(source + '[review]\nenabled="false"'), /boolean/);
});
await test('PR contract fails closed and review cannot approve blocking findings', () => {
  assert.equal(parseArtifact(artifact).commit, 'fix: write the answer');
  assert.throws(() => parseArtifact('Agent says it is done'), /invalid/);
  assert.throws(
    () => parseArtifact(artifact.replace('fix: write the answer', 'update')),
    /conventional/,
  );
  assert.throws(
    () =>
      parseReview(
        '{"verdict":"approve","findings":[{"severity":"blocking","file":"a.ts","line":1,"description":"bug"}]}',
      ),
    /disagrees/,
  );
  assert.throws(() => parseReview(''), SyntaxError);
  assert.deepEqual(parseReview('{"verdict":"approve","findings":[]}'), {
    verdict: 'approve',
    findings: [],
  });
});
