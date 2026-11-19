// 运行：node --test ip-worker/worker.test.mjs
import test from 'node:test';
import assert from 'node:assert/strict';
import worker from './worker.mjs';

const call = (path = '/', init = {}) => worker.fetch(new Request('https://ip.tar.tn' + path, init));

test('returns the address Cloudflare reports for the caller', async () => {
  for (const ip of ['203.0.113.7', '2001:db8::1']) {
    const res = await call('/', {headers: {'CF-Connecting-IP': ip}});
    assert.equal(res.status, 200);
    assert.equal(await res.text(), ip + '\n');
    assert.match(res.headers.get('Content-Type'), /^text\/plain/);
    assert.equal(res.headers.get('Cache-Control'), 'no-store');
  }
});

test('ignores the query string', async () => {
  const res = await call('/?format=text', {headers: {'CF-Connecting-IP': '203.0.113.7'}});
  assert.equal(await res.text(), '203.0.113.7\n');
});

test('fails without the address instead of answering something that is not an IP', async () => {
  const res = await call('/');
  assert.equal(res.status, 500);
});

test('answers only GET and HEAD on /', async () => {
  const headers = {'CF-Connecting-IP': '203.0.113.7'};
  assert.equal((await call('/', {method: 'HEAD', headers})).status, 200);
  const post = await call('/', {method: 'POST', headers});
  assert.equal(post.status, 405);
  assert.equal(post.headers.get('Allow'), 'GET, HEAD');
  assert.equal((await call('/admin', {headers})).status, 404);
});
