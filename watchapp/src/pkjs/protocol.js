// AppMessage command codes and flags. Keep in sync with src/c/protocol.h and docs/protocol.md §2.

module.exports = {
  // watch -> phone
  GET_STATUS: 1,
  GET_CHATS: 2,
  GET_MESSAGES: 3,
  SEND_TEXT: 4,
  MARK_READ: 5,
  GET_REPLIES: 6,
  CLOSE_CHAT: 7,
  PAIR: 8,
  GET_FULL_TEXT: 9,
  GET_IMAGE: 10,

  // phone -> watch
  STATUS: 64,
  CHAT_ITEM: 65,
  CHATS_END: 66,
  MSG_ITEM: 67,
  MSGS_END: 68,
  SEND_RESULT: 69,
  REPLY_ITEM: 70,
  REPLIES_END: 71,
  EVT_MESSAGE: 72,
  EVT_CHAT: 73,
  ERROR: 74,
  TEXT_CHUNK: 75,
  IMAGE_BEGIN: 76,
  IMAGE_CHUNK: 77,
  IMAGE_END: 78,

  // Payload bytes per TEXT_CHUNK / IMAGE_CHUNK. The watch inbox is 2048 bytes.
  CHUNK_SIZE: 1500,

  // FORMAT values for images
  FORMAT_COLOR: 0,
  FORMAT_BW: 1,

  // STATE values
  STATE_UNREACHABLE: 0,
  STATE_UNPAIRED: 1,
  STATE_PAIRING: 2,
  STATE_CONNECTING: 3,
  STATE_CONNECTED: 4,
  STATE_LOGGED_OUT: 5,
  STATE_NOT_CONFIGURED: 6,

  // ERR values
  ERR_OK: 0,
  ERR_UNREACHABLE: 1,
  ERR_NOT_PAIRED: 2,
  ERR_NOT_FOUND: 3,
  ERR_SEND_FAILED: 4,
  ERR_BAD_TOKEN: 5,
  ERR_OTHER: 6,

  // FLAGS bits. GROUP and MUTED only apply to chats; IMAGE and TRUNCATED reuse them for messages.
  FLAG_GROUP: 1 << 0,
  FLAG_MUTED: 1 << 1,
  FLAG_IMAGE: 1 << 0,
  FLAG_TRUNCATED: 1 << 1,
  FLAG_FROM_ME: 1 << 2,
  FLAG_MORE: 1 << 3,
  FLAG_MEDIA: 1 << 4,
  STATUS_SHIFT: 5,

  STATUS_CODES: { pending: 0, sent: 1, delivered: 2, read: 3, played: 3 },

  stateCode: function(state) {
    return {
      unpaired: 1, pairing: 2, connecting: 3, connected: 4, logged_out: 5
    }[state] || 0;
  }
};
