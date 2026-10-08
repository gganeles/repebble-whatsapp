// Client for the pebblewa server API (docs/protocol.md §1).

var settings = require('./settings');

function ApiError(status, code, message) {
  this.status = status;
  this.code = code;
  this.message = message;
}

function request(method, path, body, callback) {
  var s = settings.load();
  var xhr = new XMLHttpRequest();
  xhr.open(method, s.serverUrl + '/v1' + path, true);
  xhr.setRequestHeader('Authorization', 'Bearer ' + s.token);
  if (body !== undefined) {
    xhr.setRequestHeader('Content-Type', 'application/json');
  }
  xhr.timeout = 15000;
  var done = false;
  function finish(err, data) {
    if (done) {
      return;
    }
    done = true;
    callback(err, data);
  }
  xhr.onload = function() {
    var data = null;
    if (xhr.responseText) {
      try {
        data = JSON.parse(xhr.responseText);
      } catch (e) {
        return finish(new ApiError(xhr.status, 'bad_response', 'Invalid JSON from server'));
      }
    }
    if (xhr.status >= 200 && xhr.status < 300) {
      finish(null, data);
    } else {
      var e = (data && data.error) || {};
      finish(new ApiError(xhr.status, e.code || 'http_' + xhr.status, e.message || 'HTTP ' + xhr.status));
    }
  };
  xhr.onerror = function() {
    finish(new ApiError(0, 'unreachable', 'Server not reachable'));
  };
  xhr.ontimeout = function() {
    finish(new ApiError(0, 'unreachable', 'Server timed out'));
  };
  xhr.send(body !== undefined ? JSON.stringify(body) : null);
}

function q(params) {
  var parts = [];
  Object.keys(params).forEach(function(k) {
    if (params[k] !== undefined && params[k] !== null) {
      parts.push(encodeURIComponent(k) + '=' + encodeURIComponent(params[k]));
    }
  });
  return parts.length ? '?' + parts.join('&') : '';
}

function chatPath(jid) {
  return '/chats/' + encodeURIComponent(jid);
}

// Live events over WebSocket, with a polling fallback if WebSocket is missing or keeps failing.
function EventStream(onEvent, onPoll) {
  this.onEvent = onEvent;
  this.onPoll = onPoll;
  this.ws = null;
  this.failures = 0;
  this.stopped = true;
  this.timer = null;
}

EventStream.prototype.start = function() {
  this.stopped = false;
  this.connect();
};

EventStream.prototype.stop = function() {
  this.stopped = true;
  clearTimeout(this.timer);
  if (this.ws) {
    this.ws.onclose = null;
    this.ws.close();
    this.ws = null;
  }
};

EventStream.prototype.connect = function() {
  var self = this;
  if (self.stopped) {
    return;
  }
  if (typeof WebSocket === 'undefined' || self.failures >= 3) {
    self.poll();
    return;
  }
  var s = settings.load();
  var url = s.serverUrl.replace(/^http/, 'ws') + '/v1/events?token=' + encodeURIComponent(s.token);
  var opened = false;
  try {
    self.ws = new WebSocket(url);
  } catch (e) {
    self.failures = 3;
    self.poll();
    return;
  }
  self.ws.onopen = function() {
    opened = true;
    self.failures = 0;
  };
  self.ws.onmessage = function(e) {
    try {
      self.onEvent(JSON.parse(e.data));
    } catch (err) {
      console.log('Bad event: ' + err);
    }
  };
  self.ws.onclose = function() {
    self.ws = null;
    if (!opened) {
      self.failures++;
    }
    var delays = [1000, 2000, 5000, 10000];
    self.timer = setTimeout(function() { self.connect(); }, delays[Math.min(self.failures, delays.length - 1)]);
  };
};

EventStream.prototype.poll = function() {
  var self = this;
  if (self.stopped) {
    return;
  }
  self.onPoll();
  self.timer = setTimeout(function() { self.poll(); }, 15000);
};

module.exports = {
  status: function(cb) { request('GET', '/status', undefined, cb); },
  pair: function(phone, cb) { request('POST', '/pair', { phone: phone }, cb); },
  chats: function(before, limit, cb) { request('GET', '/chats' + q({ limit: limit, before: before }), undefined, cb); },
  messages: function(jid, before, limit, cb) {
    request('GET', chatPath(jid) + '/messages' + q({ limit: limit, before: before }), undefined, cb);
  },
  send: function(jid, text, clientId, cb) {
    request('POST', chatPath(jid) + '/messages', { text: text, client_id: clientId }, cb);
  },
  message: function(jid, id, cb) { request('GET', chatPath(jid) + '/messages/' + encodeURIComponent(id), undefined, cb); },
  image: function(jid, id, w, h, format, cb) {
    request('GET', chatPath(jid) + '/messages/' + encodeURIComponent(id) + '/image' + q({ w: w, h: h, format: format }), undefined, cb);
  },
  markRead: function(jid, upTo, cb) { request('POST', chatPath(jid) + '/read', { up_to: upTo }, cb); },
  replies: function(cb) { request('GET', '/replies', undefined, cb); },
  setReplies: function(replies, cb) { request('PUT', '/replies', { replies: replies }, cb); },
  EventStream: EventStream
};
