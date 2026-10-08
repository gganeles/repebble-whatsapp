// Serial AppMessage sender: one message in flight, retried on NACK.

var RETRY_DELAYS = [100, 300, 1000];

var queue = [];
var busy = false;

function pump() {
  if (busy || queue.length === 0) {
    return;
  }
  busy = true;
  var item = queue[0];
  Pebble.sendAppMessage(item.dict, function() {
    queue.shift();
    busy = false;
    pump();
  }, function() {
    busy = false;
    if (item.attempt < RETRY_DELAYS.length) {
      setTimeout(pump, RETRY_DELAYS[item.attempt]);
      item.attempt++;
    } else {
      console.log('Dropping AppMessage after retries: CMD ' + item.dict.CMD);
      queue.shift();
      pump();
    }
  });
}

module.exports = {
  send: function(dict) {
    queue.push({ dict: dict, attempt: 0 });
    pump();
  },
  // Drop queued (not in-flight) items for a request the watch no longer cares about.
  cancel: function(predicate) {
    var head = busy ? queue.slice(0, 1) : [];
    var rest = (busy ? queue.slice(1) : queue).filter(function(item) {
      return !predicate(item.dict);
    });
    queue = head.concat(rest);
  }
};
