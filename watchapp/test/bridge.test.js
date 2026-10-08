// End-to-end check of src/pkjs against a running pebblewa or pebblewa-demo server.
// Usage: node test/bridge.test.js [serverUrl] [token]
// Mocks the PebbleKit JS globals (Pebble, localStorage, XMLHttpRequest) and plays the watch.

var http = require('http');
var Module = require('module');
var path = require('path');
var assert = require('assert');

var serverUrl = process.argv[2] || 'http://127.0.0.1:8723';
var token = process.argv[3] || 'demo';
var P = require('../src/pkjs/protocol');

// NO_WS=1 exercises the polling fallback.
if (process.env.NO_WS) {
  delete global.WebSocket;
}

// --- globals PebbleKit JS provides ---
var store = {};
global.localStorage = {
  getItem: function(k) { return k in store ? store[k] : null; },
  setItem: function(k, v) { store[k] = String(v); }
};
store['pebblewa-settings'] = JSON.stringify({ serverUrl: serverUrl, token: token, phone: '+15550000000' });

global.XMLHttpRequest = function() { this.headers = {}; };
XMLHttpRequest.prototype.open = function(method, url) { this.method = method; this.url = url; };
XMLHttpRequest.prototype.setRequestHeader = function(k, v) { this.headers[k] = v; };
XMLHttpRequest.prototype.send = function(body) {
  var self = this;
  var req = http.request(self.url, { method: self.method, headers: self.headers }, function(res) {
    var data = '';
    res.on('data', function(c) { data += c; });
    res.on('end', function() { self.status = res.statusCode; self.responseText = data; self.onload(); });
  });
  req.on('error', function() { self.onerror(); });
  if (body) { req.write(body); }
  req.end();
};

var listeners = {};
var inbox = [];
var waiters = [];
global.Pebble = {
  addEventListener: function(name, fn) { listeners[name] = fn; },
  sendAppMessage: function(dict, ack) {
    inbox.push(dict);
    waiters.slice().forEach(function(w) { w(); });
    setTimeout(ack, 1);
  },
  openURL: function() {}
};

// pebble-clay isn't installed here; stub it.
var origResolve = Module._resolveFilename;
Module._resolveFilename = function(request) {
  if (request === 'pebble-clay') { return path.join(__dirname, 'clay-stub.js'); }
  return origResolve.apply(this, arguments);
};

function toWatch(payload) { listeners.appmessage({ payload: payload }); }

function waitFor(pred, label, ms) {
  return new Promise(function(resolve, reject) {
    var timer = setTimeout(function() { reject(new Error('timeout waiting for ' + label)); }, ms || 8000);
    function check() {
      var found = inbox.find(pred);
      if (found) {
        clearTimeout(timer);
        waiters.splice(waiters.indexOf(check), 1);
        resolve(found);
      }
    }
    waiters.push(check);
    check();
  });
}

async function collectChunks(cmd, req) {
  var first = await waitFor(function(d) { return d.CMD === cmd && d.REQ === req; }, 'first chunk ' + cmd);
  await waitFor(function() {
    var got = inbox.filter(function(d) { return d.CMD === cmd && d.REQ === req; });
    return got.reduce(function(n, d) { return n + (d.DATA ? d.DATA.length : 0); }, 0) >= first.TOTAL;
  }, 'all chunks ' + cmd);
  return inbox.filter(function(d) { return d.CMD === cmd && d.REQ === req; });
}

async function main() {
  require('../src/pkjs/index.js');
  listeners.ready();

  var status = await waitFor(function(d) { return d.CMD === P.STATUS; }, 'STATUS');
  assert.strictEqual(status.STATE, P.STATE_CONNECTED, 'connected: ' + JSON.stringify(status));

  toWatch({ CMD: P.GET_CHATS, REQ: 1, INDEX: 0 });
  var end = await waitFor(function(d) { return d.CMD === P.CHATS_END && d.REQ === 1; }, 'CHATS_END');
  var chats = inbox.filter(function(d) { return d.CMD === P.CHAT_ITEM && d.REQ === 1; });
  assert.strictEqual(chats.length, 10);
  assert.ok(end.FLAGS & P.FLAG_MORE, 'more pages');
  chats.forEach(function(c) {
    assert.ok(Buffer.byteLength(c.NAME) < 32 && Buffer.byteLength(c.TEXT) < 64, 'limits: ' + JSON.stringify(c));
  });
  console.log('chats page 1:', chats.map(function(c) { return c.NAME + ' (' + c.UNREAD + ')'; }).join(', '));

  toWatch({ CMD: P.GET_CHATS, REQ: 2, INDEX: 1 });
  await waitFor(function(d) { return d.CMD === P.CHATS_END && d.REQ === 2; }, 'page 2');
  var page2 = inbox.filter(function(d) { return d.CMD === P.CHAT_ITEM && d.REQ === 2; });
  assert.strictEqual(page2.length, 10);
  assert.strictEqual(page2[0].INDEX, 10);

  var alice = chats.find(function(c) { return c.NAME === 'Alice Martin'; });
  assert.ok(alice, 'Alice in first page');
  toWatch({ CMD: P.GET_MESSAGES, REQ: 3, CHAT: alice.CHAT });
  var mend = await waitFor(function(d) { return d.CMD === P.MSGS_END && d.REQ === 3; }, 'MSGS_END');
  var msgs = inbox.filter(function(d) { return d.CMD === P.MSG_ITEM && d.REQ === 3; });
  assert.strictEqual(msgs.length, 15);
  assert.ok(mend.FLAGS & P.FLAG_MORE);
  assert.ok(msgs[msgs.length - 1].TEXT.indexOf('[Photo]') === 0 && (msgs[msgs.length - 1].FLAGS & P.FLAG_MEDIA));
  console.log('newest message:', msgs[msgs.length - 1].TEXT);

  // Full text: the long message is cut in the list and fetched in chunks.
  var longMsg = msgs.find(function(d) { return d.FLAGS & P.FLAG_TRUNCATED; });
  assert.ok(longMsg, 'a truncated message');
  assert.ok(Buffer.byteLength(longMsg.TEXT) < 320);
  toWatch({ CMD: P.GET_FULL_TEXT, REQ: 20, CHAT: alice.CHAT, MSG: longMsg.MSG });
  var textChunks = await collectChunks(P.TEXT_CHUNK, 20);
  var full = Buffer.concat(textChunks.map(function(d) { return Buffer.from(d.DATA); })).toString('utf8');
  assert.strictEqual(Buffer.concat(textChunks.map(function(d) { return Buffer.from(d.DATA); })).length, textChunks[0].TOTAL);
  assert.ok(full.length > 600 && /tell you the rest tonight!$/.test(full), 'full text: ' + full.slice(-40));
  console.log('full text:', Buffer.byteLength(full), 'bytes in', textChunks.length, 'chunks');

  // Image: begin, chunks, end; reassemble the way the watch does.
  var photo = msgs.find(function(d) { return d.FLAGS & P.FLAG_IMAGE; });
  assert.ok(photo, 'a message with a picture');
  toWatch({ CMD: P.GET_IMAGE, REQ: 21, CHAT: alice.CHAT, MSG: photo.MSG, WIDTH: 192, HEIGHT: 192, FORMAT: P.FORMAT_COLOR });
  var begin = await waitFor(function(d) { return d.CMD === P.IMAGE_BEGIN && d.REQ === 21; }, 'IMAGE_BEGIN', 15000);
  await waitFor(function(d) { return d.CMD === P.IMAGE_END && d.REQ === 21; }, 'IMAGE_END', 15000);
  var pixels = Buffer.alloc(begin.TOTAL);
  var imgChunks = inbox.filter(function(d) { return d.CMD === P.IMAGE_CHUNK && d.REQ === 21; });
  imgChunks.forEach(function(d) { Buffer.from(d.DATA).copy(pixels, d.OFFSET); });
  assert.strictEqual(begin.TOTAL, begin.WIDTH * begin.HEIGHT);
  assert.ok(begin.WIDTH === 192 && begin.HEIGHT === 144, begin.WIDTH + 'x' + begin.HEIGHT);
  console.log('image:', begin.WIDTH + 'x' + begin.HEIGHT, begin.TOTAL, 'bytes in', imgChunks.length, 'chunks');
  if (process.env.IMAGE_OUT) {
    // Write a PPM preview of what the watch will draw.
    var rgb = Buffer.alloc(begin.TOTAL * 3);
    for (var i = 0; i < begin.TOTAL; i++) {
      var px = pixels[i];
      rgb[i * 3] = ((px >> 4) & 3) * 85;
      rgb[i * 3 + 1] = ((px >> 2) & 3) * 85;
      rgb[i * 3 + 2] = (px & 3) * 85;
    }
    require('fs').writeFileSync(process.env.IMAGE_OUT,
      Buffer.concat([Buffer.from('P6\n' + begin.WIDTH + ' ' + begin.HEIGHT + '\n255\n'), rgb]));
  }

  toWatch({ CMD: P.GET_MESSAGES, REQ: 4, CHAT: alice.CHAT, MSG: msgs[0].MSG });
  await waitFor(function(d) { return d.CMD === P.MSGS_END && d.REQ === 4; }, 'older MSGS_END');
  var older = inbox.filter(function(d) { return d.CMD === P.MSG_ITEM && d.REQ === 4; });
  assert.strictEqual(older.length, 15);
  assert.ok(older[older.length - 1].TS <= msgs[0].TS, 'older page is older');

  toWatch({ CMD: P.MARK_READ, REQ: 5, CHAT: alice.CHAT });
  await waitFor(function(d) { return d.CMD === P.EVT_CHAT && d.CHAT === alice.CHAT && d.UNREAD === 0; }, 'unread cleared', 20000);

  toWatch({ CMD: P.GET_REPLIES, REQ: 6 });
  await waitFor(function(d) { return d.CMD === P.REPLIES_END && d.REQ === 6; }, 'REPLIES_END');
  var replies = inbox.filter(function(d) { return d.CMD === P.REPLY_ITEM; });
  assert.ok(replies.length > 0 && replies.length <= 8);

  toWatch({ CMD: P.SEND_TEXT, REQ: 7, CHAT: alice.CHAT, TEXT: replies[1].TEXT });
  var result = await waitFor(function(d) { return d.CMD === P.SEND_RESULT && d.REQ === 7; }, 'SEND_RESULT');
  assert.strictEqual(result.ERR, P.ERR_OK);
  assert.ok(result.MSG > 0);

  // The demo contact answers after ~2s, which should arrive as a live event for the open chat.
  var evt = await waitFor(function(d) {
    return d.CMD === P.EVT_MESSAGE && !(d.FLAGS & P.FLAG_FROM_ME);
  }, 'live reply', 20000);
  console.log('live reply:', evt.TEXT, typeof WebSocket !== 'undefined' ? '(WebSocket)' : '(polling)');

  toWatch({ CMD: P.CLOSE_CHAT, REQ: 8, CHAT: alice.CHAT });
  console.log('bridge test passed');
  process.exit(0);
}

main().catch(function(e) {
  console.error('FAIL:', e.message);
  console.error('last messages:', JSON.stringify(inbox.slice(-5)));
  process.exit(1);
});
