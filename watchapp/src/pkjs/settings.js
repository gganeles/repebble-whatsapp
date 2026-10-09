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
  // Clay also keeps its own copy of the last saved page; use it for anything we missed.
  var clay = {};
  try {
    clay = JSON.parse(localStorage.getItem('clay-settings') || '{}') || {};
  } catch (e) {
    clay = {};
  }
  var out = {};
  Object.keys(DEFAULTS).forEach(function(k) {
    var v = saved[k];
    if (v === undefined || v === '' || (Array.isArray(v) && !v.length)) {
      v = typeof clay[k] === 'string' ? clay[k].trim() : clay[k];
    }
    out[k] = v !== undefined && v !== '' && v !== null ? v : DEFAULTS[k];
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
