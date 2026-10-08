#pragma once
// AppMessage command codes and flags. Keep in sync with src/pkjs/protocol.js and docs/protocol.md §2.

// watch -> phone
#define CMD_GET_STATUS   1
#define CMD_GET_CHATS    2
#define CMD_GET_MESSAGES 3
#define CMD_SEND_TEXT    4
#define CMD_MARK_READ    5
#define CMD_GET_REPLIES  6
#define CMD_CLOSE_CHAT   7
#define CMD_PAIR         8
#define CMD_GET_FULL_TEXT 9
#define CMD_GET_IMAGE    10

// phone -> watch
#define CMD_STATUS       64
#define CMD_CHAT_ITEM    65
#define CMD_CHATS_END    66
#define CMD_MSG_ITEM     67
#define CMD_MSGS_END     68
#define CMD_SEND_RESULT  69
#define CMD_REPLY_ITEM   70
#define CMD_REPLIES_END  71
#define CMD_EVT_MESSAGE  72
#define CMD_EVT_CHAT     73
#define CMD_ERROR        74
#define CMD_TEXT_CHUNK   75
#define CMD_IMAGE_BEGIN  76
#define CMD_IMAGE_CHUNK  77
#define CMD_IMAGE_END    78

#define FORMAT_COLOR 0
#define FORMAT_BW    1

typedef enum {
  STATE_UNREACHABLE = 0,
  STATE_UNPAIRED = 1,
  STATE_PAIRING = 2,
  STATE_CONNECTING = 3,
  STATE_CONNECTED = 4,
  STATE_LOGGED_OUT = 5,
  STATE_NOT_CONFIGURED = 6,
  STATE_UNKNOWN = 255,
} ServerState;

#define ERR_OK          0
#define ERR_UNREACHABLE 1
#define ERR_NOT_PAIRED  2
#define ERR_NOT_FOUND   3
#define ERR_SEND_FAILED 4
#define ERR_BAD_TOKEN   5
#define ERR_OTHER       6

#define FLAG_GROUP   (1 << 0)
#define FLAG_MUTED   (1 << 1)
// Message-only meanings of bits 0 and 1 (GROUP/MUTED only apply to chats).
#define FLAG_IMAGE     (1 << 0)
#define FLAG_TRUNCATED (1 << 1)
#define FLAG_FROM_ME (1 << 2)
#define FLAG_MORE    (1 << 3)
#define FLAG_MEDIA   (1 << 4)
#define FLAG_STATUS_SHIFT 5
#define FLAG_STATUS_MASK  (3 << FLAG_STATUS_SHIFT)
// Watch-local: our own message failed to send. Never sent by the phone.
#define FLAG_FAILED  (1 << 7)

#define MSG_STATUS_PENDING   0
#define MSG_STATUS_SENT      1
#define MSG_STATUS_DELIVERED 2
#define MSG_STATUS_READ      3
