// PebbleKit JS bridge: watch AppMessages <-> pebblewa HTTP API.
// See docs/protocol.md for the message schema.

var Clay = require('pebble-clay');
var clayConfig = require('./config');
var P = require('./protocol');
var api = require('./api');
var bytes = require('./bytes');
var handles = require('./handles');
var queue = require('./queue');
var settings = require('./settings');

var clay = new Clay(clayConfig, null, { autoHandleEvents: false });

var CHAT_PAGE = 10;
var MSG_PAGE = 15;

var chatCursors = [null]; // page index -> "before" cursor for that page
var openChat = null; // jid of the conversation the watch is showing
var latestReq = {}; // CMD -> latest REQ, to drop stale work
var pollState = { chatSeen: {}, lastMsgId: null }; // chatSeen: jid -> "ts:unread"

function chatVersion(chat) {
  return chat.ts + ':' + chat.unread;
}

function send(dict) {
  queue.send(dict);
}

function errCode(err, fallback) {
  if (!err) {
    return P.ERR_OK;
  }
  if (err.status === 0) {
    return P.ERR_UNREACHABLE;
  }
  switch (err.code) {
    case 'bad_token': return P.ERR_BAD_TOKEN;
    case 'not_paired': return P.ERR_NOT_PAIRED;
    case 'not_found': return P.ERR_NOT_FOUND;
  }
  return fallback || P.ERR_OTHER;
}

function sendError(req, err, fallback) {
  send({ CMD: P.ERROR, REQ: req || 0, ERR: errCode(err, fallback), TEXT: (err && err.message || 'Error').slice(0, 100) });
}

function statusFlags(status) {
  return (P.STATUS_CODES[status] || 0) << P.STATUS_SHIFT;
}

function chatDict(chat, req, index, cmd) {
  var flags = (chat.group ? P.FLAG_GROUP : 0) | (chat.muted ? P.FLAG_MUTED : 0) | (chat.from_me ? P.FLAG_FROM_ME : 0);
  return {
    CMD: cmd,
    REQ: req,
    INDEX: index,
    CHAT: handles.chats.get(chat.jid),
    NAME: chat.name,
    TEXT: chat.preview,
    TS: chat.ts,
    UNREAD: chat.unread,
    FLAGS: flags
  };
}

function messageDict(jid, msg, req, index, cmd) {
  var flags = (msg.from_me ? P.FLAG_FROM_ME : 0) | (msg.kind !== 'text' ? P.FLAG_MEDIA : 0) | statusFlags(msg.status) |
    (msg.has_image ? P.FLAG_IMAGE : 0) | (msg.truncated ? P.FLAG_TRUNCATED : 0);
  var dict = {
    CMD: cmd,
    REQ: req,
    INDEX: index,
    CHAT: handles.chats.get(jid),
    MSG: handles.messages.get(handles.messageKey(jid, msg.id)),
    TEXT: msg.text,
    TS: msg.ts,
    FLAGS: flags
  };
  if (msg.sender) {
    dict.NAME = msg.sender;
  }
  return dict;
}

// ---- Status and pairing ----

function sendStatus(err, statusObj) {
  if (err) {
    var state = err.code === 'bad_token' ? P.STATE_NOT_CONFIGURED : P.STATE_UNREACHABLE;
    send({ CMD: P.STATUS, STATE: state, ERR: errCode(err), TEXT: err.message.slice(0, 100) });
    return;
  }
  var text = statusObj.pairing_code || statusObj.phone || '';
  if (statusObj.state === 'unpaired' && !settings.load().phone) {
    text = 'Set your number in the app settings';
  }
  send({ CMD: P.STATUS, STATE: P.stateCode(statusObj.state), ERR: P.ERR_OK, TEXT: text });
}

function getStatus() {
  var s = settings.load();
  console.log('Checking status at ' + s.serverUrl + ' with token of length ' + s.token.length);
  if (!settings.load().token) {
    console.log('No token saved; settings: ' + JSON.stringify(settings.load()));
    send({ CMD: P.STATUS, STATE: P.STATE_NOT_CONFIGURED, ERR: P.ERR_BAD_TOKEN, TEXT: 'No token saved. Open app settings on your phone.' });
    return;
  }
  api.status(sendStatus);
}

function pair(req) {
  var phone = settings.load().phone;
  if (!phone) {
    send({ CMD: P.STATUS, STATE: P.STATE_UNPAIRED, ERR: P.ERR_OK, TEXT: 'Set your number in the app settings' });
    return;
  }
  api.pair(phone, function(err, data) {
    if (err) {
      return sendError(req, err);
    }
    send({ CMD: P.STATUS, STATE: P.STATE_PAIRING, ERR: P.ERR_OK, TEXT: data.pairing_code });
  });
}

// ---- Chats ----

function getChats(req, page) {
  latestReq[P.GET_CHATS] = req;
  if (page === 0) {
    chatCursors = [null];
  }
  if (page > 0 && !chatCursors[page]) {
    send({ CMD: P.CHATS_END, REQ: req, FLAGS: 0 });
    return;
  }
  api.chats(chatCursors[page], CHAT_PAGE, function(err, data) {
    if (latestReq[P.GET_CHATS] !== req) {
      return;
    }
    if (err) {
      return sendError(req, err);
    }
    data.chats.forEach(function(chat, i) {
      pollState.chatSeen[chat.jid] = chatVersion(chat);
      send(chatDict(chat, req, page * CHAT_PAGE + i, P.CHAT_ITEM));
    });
    chatCursors[page + 1] = data.next;
    send({ CMD: P.CHATS_END, REQ: req, FLAGS: data.next ? P.FLAG_MORE : 0 });
  });
}

// ---- Messages ----

function getMessages(req, chatHandle, beforeHandle) {
  var jid = handles.chats.lookup(chatHandle);
  if (!jid) {
    return sendError(req, { status: 404, code: 'not_found', message: 'Unknown chat' });
  }
  latestReq[P.GET_MESSAGES] = req;
  queue.cancel(function(d) {
    return (d.CMD === P.MSG_ITEM || d.CMD === P.MSGS_END) && d.REQ !== req;
  });
  openChat = jid;
  var before = beforeHandle ? handles.messageId(handles.messages.lookup(beforeHandle)) : undefined;
  api.messages(jid, before, MSG_PAGE, function(err, data) {
    if (latestReq[P.GET_MESSAGES] !== req) {
      return;
    }
    if (err) {
      return sendError(req, err);
    }
    data.messages.forEach(function(msg, i) {
      send(messageDict(jid, msg, req, i, P.MSG_ITEM));
    });
    if (!before && data.messages.length) {
      pollState.lastMsgId = data.messages[data.messages.length - 1].id;
    }
    send({ CMD: P.MSGS_END, REQ: req, CHAT: chatHandle, FLAGS: data.next ? P.FLAG_MORE : 0 });
  });
}

function sendText(req, chatHandle, text) {
  var jid = handles.chats.lookup(chatHandle);
  if (!jid || !text) {
    return send({ CMD: P.SEND_RESULT, REQ: req, CHAT: chatHandle, ERR: P.ERR_NOT_FOUND });
  }
  var clientId = 'w' + Date.now().toString(36) + '-' + req;
  api.send(jid, text, clientId, function(err, data) {
    if (err) {
      return send({ CMD: P.SEND_RESULT, REQ: req, CHAT: chatHandle, ERR: errCode(err, P.ERR_SEND_FAILED) });
    }
    send({
      CMD: P.SEND_RESULT,
      REQ: req,
      CHAT: chatHandle,
      MSG: handles.messages.get(handles.messageKey(jid, data.id)),
      TS: data.ts,
      ERR: P.ERR_OK
    });
  });
}

function markRead(req, chatHandle, msgHandle) {
  var jid = handles.chats.lookup(chatHandle);
  if (!jid) {
    return;
  }
  var upTo = msgHandle ? handles.messageId(handles.messages.lookup(msgHandle)) : undefined;
  api.markRead(jid, upTo, function(err) {
    if (err) {
      console.log('Mark read failed: ' + err.message);
    }
  });
}

// ---- Full text and images (chunked) ----

function lookupMessage(chatHandle, msgHandle) {
  var jid = handles.chats.lookup(chatHandle);
  var id = handles.messageId(handles.messages.lookup(msgHandle));
  return jid && id ? { jid: jid, id: id } : null;
}

// Only one detail view is open at a time, so drop chunks for older requests.
function startMediaRequest(req) {
  latestReq.media = req;
  queue.cancel(function(d) {
    return (d.CMD === P.TEXT_CHUNK || d.CMD === P.IMAGE_BEGIN || d.CMD === P.IMAGE_CHUNK || d.CMD === P.IMAGE_END) &&
      d.REQ !== req;
  });
}

// Sends data as consecutive chunks; an empty payload is one chunk with TOTAL 0 and no DATA.
function sendChunks(cmd, req, data, extra) {
  var off = 0;
  do {
    var dict = { CMD: cmd, REQ: req, OFFSET: off, TOTAL: data.length };
    if (data.length) {
      dict.DATA = data.slice(off, off + P.CHUNK_SIZE);
    }
    Object.keys(extra || {}).forEach(function(k) { dict[k] = extra[k]; });
    send(dict);
    off += P.CHUNK_SIZE;
  } while (off < data.length);
}

function getFullText(req, chatHandle, msgHandle) {
  var m = lookupMessage(chatHandle, msgHandle);
  if (!m) {
    return sendError(req, { status: 404, code: 'not_found', message: 'Unknown message' });
  }
  startMediaRequest(req);
  api.message(m.jid, m.id, function(err, data) {
    if (latestReq.media !== req) {
      return;
    }
    if (err) {
      return sendError(req, err);
    }
    sendChunks(P.TEXT_CHUNK, req, bytes.utf8Bytes(data.message.text), { MSG: msgHandle });
  });
}

function getImage(req, chatHandle, msgHandle, width, height, format) {
  var m = lookupMessage(chatHandle, msgHandle);
  if (!m) {
    return sendError(req, { status: 404, code: 'not_found', message: 'Unknown message' });
  }
  startMediaRequest(req);
  var fmt = format === P.FORMAT_BW ? 'bw' : 'color';
  api.image(m.jid, m.id, width, height, fmt, function(err, data) {
    if (latestReq.media !== req) {
      return;
    }
    if (err) {
      return sendError(req, err);
    }
    var pixels = bytes.base64ToBytes(data.data);
    send({
      CMD: P.IMAGE_BEGIN,
      REQ: req,
      MSG: msgHandle,
      WIDTH: data.width,
      HEIGHT: data.height,
      FORMAT: data.format === 'bw' ? P.FORMAT_BW : P.FORMAT_COLOR,
      STRIDE: data.row_bytes,
      TOTAL: pixels.length
    });
    for (var off = 0; off < pixels.length; off += P.CHUNK_SIZE) {
      send({ CMD: P.IMAGE_CHUNK, REQ: req, OFFSET: off, DATA: pixels.slice(off, off + P.CHUNK_SIZE) });
    }
    send({ CMD: P.IMAGE_END, REQ: req, MSG: msgHandle });
  });
}

// ---- Replies ----

function getReplies(req) {
  api.replies(function(err, data) {
    var replies = err ? ['OK', 'On my way', 'Yes', 'No'] : data.replies;
    replies.slice(0, 8).forEach(function(text, i) {
      send({ CMD: P.REPLY_ITEM, REQ: req, INDEX: i, TEXT: text });
    });
    send({ CMD: P.REPLIES_END, REQ: req });
  });
}

// ---- Live updates ----

function onServerEvent(evt) {
  switch (evt.type) {
    case 'message':
      if (evt.chat === openChat) {
        send(messageDict(evt.chat, evt.message, 0, 0, P.EVT_MESSAGE));
      }
      break;
    case 'chat':
      pollState.chatSeen[evt.chat.jid] = chatVersion(evt.chat);
      send(chatDict(evt.chat, 0, 0, P.EVT_CHAT));
      break;
    case 'connection':
      api.status(sendStatus);
      break;
  }
}

// Polling fallback when WebSocket isn't available.
function poll() {
  api.chats(null, CHAT_PAGE, function(err, data) {
    if (err) {
      return;
    }
    data.chats.forEach(function(chat) {
      if (pollState.chatSeen[chat.jid] !== chatVersion(chat)) {
        pollState.chatSeen[chat.jid] = chatVersion(chat);
        send(chatDict(chat, 0, 0, P.EVT_CHAT));
      }
    });
  });
  if (openChat) {
    var jid = openChat;
    api.messages(jid, undefined, MSG_PAGE, function(err, data) {
      if (err || jid !== openChat) {
        return;
      }
      var msgs = data.messages;
      var start = 0;
      for (var i = 0; i < msgs.length; i++) {
        if (msgs[i].id === pollState.lastMsgId) {
          start = i + 1;
        }
      }
      msgs.slice(start).forEach(function(msg) {
        send(messageDict(jid, msg, 0, 0, P.EVT_MESSAGE));
      });
      if (msgs.length) {
        pollState.lastMsgId = msgs[msgs.length - 1].id;
      }
    });
  }
}

var stream = new api.EventStream(onServerEvent, poll);

// ---- Pebble events ----

Pebble.addEventListener('ready', function() {
  console.log('pebblewa bridge ready');
  getStatus();
  if (settings.load().token) {
    stream.start();
  }
});

Pebble.addEventListener('appmessage', function(e) {
  var p = e.payload;
  var req = p.REQ || 0;
  switch (p.CMD) {
    case P.GET_STATUS: getStatus(); break;
    case P.PAIR: pair(req); break;
    case P.GET_CHATS: getChats(req, p.INDEX || 0); break;
    case P.GET_MESSAGES: getMessages(req, p.CHAT, p.MSG); break;
    case P.SEND_TEXT: sendText(req, p.CHAT, p.TEXT); break;
    case P.MARK_READ: markRead(req, p.CHAT, p.MSG); break;
    case P.GET_REPLIES: getReplies(req); break;
    case P.GET_FULL_TEXT: getFullText(req, p.CHAT, p.MSG); break;
    case P.GET_IMAGE: getImage(req, p.CHAT, p.MSG, p.WIDTH, p.HEIGHT, p.FORMAT); break;
    case P.CLOSE_CHAT:
      openChat = null;
      pollState.lastMsgId = null;
      break;
    default:
      console.log('Unknown command ' + p.CMD);
  }
});

Pebble.addEventListener('showConfiguration', function() {
  console.log('Opening settings page');
  Pebble.openURL(clay.generateUrl());
});

Pebble.addEventListener('webviewclosed', function(e) {
  console.log('Settings page closed, response length ' + (e && e.response ? e.response.length : 0));
  if (!e || !e.response) {
    return;
  }
  // Clay keys settings by messageKey. Fall back to decoding the response ourselves.
  var raw = {};
  try {
    raw = clay.getSettings(e.response, false) || {};
  } catch (err) {
    console.log('Clay getSettings failed: ' + err);
  }
  if (!Object.keys(raw).length) {
    try {
      raw = JSON.parse(decodeURIComponent(e.response)) || {};
    } catch (err) {
      console.log('Could not parse settings: ' + err);
      return;
    }
  }
  var values = {};
  Object.keys(raw).forEach(function(k) {
    var v = raw[k];
    values[k] = v && typeof v === 'object' && 'value' in v ? v.value : v;
  });
  var replies = [];
  for (var n = 1; n <= 8; n++) {
    var r = String(values['reply' + n] || '').trim();
    if (r) {
      replies.push(r);
    }
  }
  // Only overwrite fields that came back filled in.
  var update = { replies: replies };
  ['serverUrl', 'token', 'phone'].forEach(function(k) {
    var v = String(values[k] || '').trim();
    if (v) {
      update[k] = v;
    }
  });
  console.log('Saved settings: ' + Object.keys(update).join(', '));
  settings.save(update);
  if (replies.length) {
    api.setReplies(replies, function(err) {
      if (err) {
        console.log('Saving replies failed: ' + err.message);
      }
    });
  }
  stream.stop();
  stream.failures = 0;
  stream.start();
  getStatus();
});
