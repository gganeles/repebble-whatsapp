// Maps long WhatsApp ids to small integers so the watch never stores JIDs.
// Handles are only valid for one run of the watchapp.

function HandleMap() {
  this.byKey = {};
  this.byHandle = {};
  this.next = 1;
}

HandleMap.prototype.get = function(key) {
  var h = this.byKey[key];
  if (!h) {
    h = this.next++;
    if (this.next > 0xffff) {
      this.next = 1;
    }
    this.byKey[key] = h;
    this.byHandle[h] = key;
  }
  return h;
};

HandleMap.prototype.lookup = function(handle) {
  return this.byHandle[handle];
};

module.exports = {
  chats: new HandleMap(),
  // Message handles are per chat key: "<jid>\n<message id>".
  messages: new HandleMap(),
  messageKey: function(jid, id) {
    return jid + '\n' + id;
  },
  messageId: function(key) {
    return key ? key.split('\n')[1] : undefined;
  }
};
