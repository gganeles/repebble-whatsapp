#pragma once
#include <pebble.h>

// Field sizes include the NUL. Keep in sync with server/internal/watchtext limits.
#define CHAT_NAME_LEN   32
#define CHAT_PREVIEW_LEN 64
#define SENDER_LEN      24
#define MSG_TEXT_LEN    320
#define REPLY_LEN       40

#define MAX_CHATS    30
#define MAX_MESSAGES 20
#define MAX_REPLIES  8

typedef struct {
  uint16_t handle;
  char name[CHAT_NAME_LEN];
  char preview[CHAT_PREVIEW_LEN];
  int32_t ts;
  uint8_t unread;
  uint8_t flags;
} Chat;

typedef struct {
  uint16_t handle;  // 0 while our own message is still sending
  uint8_t req;      // request id of a pending send
  char sender[SENDER_LEN];
  char text[MSG_TEXT_LEN];
  int32_t ts;
  uint8_t flags;
} Message;

typedef struct {
  Chat chats[MAX_CHATS];
  int chat_count;
  bool chats_more;      // server has another page
  bool chats_loading;
  int chat_pages;       // pages loaded so far

  uint16_t open_chat;   // handle of the conversation being shown, 0 if none
  Message messages[MAX_MESSAGES];
  int message_count;
  bool messages_more;   // older messages available
  bool messages_loading;
  bool messages_loaded; // first page arrived

  char replies[MAX_REPLIES][REPLY_LEN];
  int reply_count;
  bool replies_loaded;
} Model;

extern Model g_model;

Chat *model_find_chat(uint16_t handle);
// Inserts or updates a chat. If resort is true the list is re-sorted newest first.
Chat *model_upsert_chat(const Chat *chat, bool resort);
void model_clear_chats(void);

void model_clear_messages(uint16_t chat);
Message *model_find_message(uint16_t handle);
// Appends at the end (newest); drops the oldest when full.
Message *model_append_message(const Message *msg);
// Inserts at position index (used while loading an older page); drops the newest when full.
Message *model_insert_message(int index, const Message *msg);

void model_copy(char *dest, const char *src, size_t size);
