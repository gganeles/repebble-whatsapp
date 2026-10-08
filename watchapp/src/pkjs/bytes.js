// Byte helpers: PebbleKit JS has no TextEncoder or reliable atob.

var B64 = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/';
var B64_LOOKUP = {};
for (var i = 0; i < B64.length; i++) {
  B64_LOOKUP[B64.charAt(i)] = i;
}

function base64ToBytes(s) {
  s = s.replace(/[^A-Za-z0-9+/]/g, '');
  var out = [];
  var buf = 0;
  var bits = 0;
  for (var i = 0; i < s.length; i++) {
    buf = (buf << 6) | B64_LOOKUP[s.charAt(i)];
    bits += 6;
    if (bits >= 8) {
      bits -= 8;
      out.push((buf >> bits) & 0xff);
    }
  }
  return out;
}

function utf8Bytes(str) {
  var bin = unescape(encodeURIComponent(str));
  var out = new Array(bin.length);
  for (var i = 0; i < bin.length; i++) {
    out[i] = bin.charCodeAt(i);
  }
  return out;
}

module.exports = { base64ToBytes: base64ToBytes, utf8Bytes: utf8Bytes };
