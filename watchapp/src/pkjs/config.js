// Clay config page definition.
module.exports = [
  {
    type: 'heading',
    defaultValue: 'WhatsApp for Pebble'
  },
  {
    type: 'text',
    defaultValue: 'Run <code>pebblewa</code> in Termux, then paste the token it prints (or run <code>pebblewa token</code>).'
  },
  {
    type: 'section',
    items: [
      { type: 'heading', defaultValue: 'Server' },
      {
        type: 'input',
        id: 'serverUrl',
        label: 'Server URL',
        defaultValue: 'http://127.0.0.1:8723',
        attributes: { type: 'url' }
      },
      {
        type: 'input',
        id: 'token',
        label: 'Token',
        defaultValue: '',
        attributes: { autocapitalize: 'off', autocorrect: 'off' }
      }
    ]
  },
  {
    type: 'section',
    items: [
      { type: 'heading', defaultValue: 'Linking' },
      {
        type: 'input',
        id: 'phone',
        label: 'Your WhatsApp number',
        description: 'International format, e.g. +491512345678. Used to get a pairing code shown on the watch.',
        defaultValue: '',
        attributes: { type: 'tel' }
      }
    ]
  },
  {
    type: 'section',
    items: [
      { type: 'heading', defaultValue: 'Quick replies' },
      {
        type: 'text',
        defaultValue: 'Leave all empty to keep the list stored on the server.'
      }
    ].concat([1, 2, 3, 4, 5, 6, 7, 8].map(function(n) {
      return {
        type: 'input',
        id: 'reply' + n,
        label: 'Reply ' + n,
        defaultValue: '',
        attributes: { maxlength: 39 }
      };
    }))
  },
  {
    type: 'submit',
    defaultValue: 'Save'
  }
];
