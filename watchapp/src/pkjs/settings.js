// User settings, saved from the Clay config page.

var KEY = 'pebblewa-settings';
var DEFAULTS = {
  serverUrl: 'http://127.0.0.1:8723',
  token: '',
  phone: '',
  replies: []
};

function load() {
  var saved = {};
  try {
    saved = JSON.parse(localStorage.getItem(KEY) || '{}') || {};
  } catch (e) {
    saved = {};
  }
  var out = {};
  Object.keys(DEFAULTS).forEach(function(k) {
    out[k] = saved[k] !== undefined && saved[k] !== '' ? saved[k] : DEFAULTS[k];
  });
  out.serverUrl = String(out.serverUrl).replace(/\/+$/, '');
  return out;
}

function save(values) {
  var current = load();
  Object.keys(values).forEach(function(k) {
    if (k in DEFAULTS) {
      current[k] = values[k];
    }
  });
  localStorage.setItem(KEY, JSON.stringify(current));
  return current;
}

module.exports = { load: load, save: save, DEFAULTS: DEFAULTS };
