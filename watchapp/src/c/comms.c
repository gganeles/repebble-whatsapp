#include "comms.h"
#include "model.h"
#include "protocol.h"
#include "ui.h"

#define OUTBOX_QUEUE 6
#define MAX_RETRIES 3

typedef struct {
  uint8_t cmd;
  uint8_t req;
  uint16_t chat;
  uint16_t msg;
  int16_t index;   // -1 if unused
  char *text;      // heap copy, SEND_TEXT only
  uint8_t width;   // GET_IMAGE only (0 = unused)
  uint8_t height;
  uint8_t format;
} Outgoing;

static Outgoing s_queue[OUTBOX_QUEUE];
static int s_queue_len;
static bool s_in_flight;
static int s_retries;
static AppTimer *s_retry_timer;

static uint8_t s_next_req = 1;
static uint8_t s_chats_req;
static uint8_t s_msgs_req;
static uint8_t s_replies_req;
static bool s_msgs_older;   // current messages request is for an older page
static int s_insert_at;     // next insert position while loading an older page

static uint8_t next_req(void) {
  uint8_t r = s_next_req++;
  if (s_next_req == 0) {
    s_next_req = 1;
  }
  return r;
}

// ---- Outbox ----

static void pump(void);

static void retry_cb(void *data) {
  s_retry_timer = NULL;
  pump();
}

static void pop_front(void) {
  free(s_queue[0].text);
  memmove(&s_queue[0], &s_queue[1], sizeof(Outgoing) * (s_queue_len - 1));
  s_queue_len--;
  s_retries = 0;
}

static void pump(void) {
  if (s_in_flight || s_queue_len == 0 || s_retry_timer) {
    return;
  }
  Outgoing *o = &s_queue[0];
  DictionaryIterator *iter;
  if (app_message_outbox_begin(&iter) != APP_MSG_OK) {
    s_retry_timer = app_timer_register(200, retry_cb, NULL);
    return;
  }
  dict_write_uint8(iter, MESSAGE_KEY_CMD, o->cmd);
  dict_write_uint8(iter, MESSAGE_KEY_REQ, o->req);
  if (o->chat) {
    dict_write_uint16(iter, MESSAGE_KEY_CHAT, o->chat);
  }
  if (o->msg) {
    dict_write_uint16(iter, MESSAGE_KEY_MSG, o->msg);
  }
  if (o->index >= 0) {
    dict_write_uint8(iter, MESSAGE_KEY_INDEX, (uint8_t)o->index);
  }
  if (o->text) {
    dict_write_cstring(iter, MESSAGE_KEY_TEXT, o->text);
  }
  if (o->width) {
    dict_write_uint8(iter, MESSAGE_KEY_WIDTH, o->width);
    dict_write_uint8(iter, MESSAGE_KEY_HEIGHT, o->height);
    dict_write_uint8(iter, MESSAGE_KEY_FORMAT, o->format);
  }
  if (app_message_outbox_send() == APP_MSG_OK) {
    s_in_flight = true;
  } else {
    s_retry_timer = app_timer_register(200, retry_cb, NULL);
  }
}

static void outbox_sent(DictionaryIterator *iter, void *context) {
  s_in_flight = false;
  pop_front();
  pump();
}

static void outbox_failed(DictionaryIterator *iter, AppMessageResult reason, void *context) {
  s_in_flight = false;
  APP_LOG(APP_LOG_LEVEL_WARNING, "Outbox failed: %d", (int)reason);
  if (++s_retries > MAX_RETRIES) {
    if (s_queue[0].cmd == CMD_SEND_TEXT) {
      // Mark the optimistic message as failed.
      for (int i = 0; i < g_model.message_count; i++) {
        Message *m = &g_model.messages[i];
        if (m->handle == 0 && m->req == s_queue[0].req) {
          m->flags |= FLAG_FAILED;
        }
      }
      conversation_window_refresh(false);
      vibes_double_pulse();
    }
    pop_front();
  }
  s_retry_timer = app_timer_register(300 * s_retries + 100, retry_cb, NULL);
}

static uint8_t enqueue_ex(uint8_t cmd, uint16_t chat, uint16_t msg, int index, const char *text,
                          uint8_t width, uint8_t height, uint8_t format) {
  if (s_queue_len == OUTBOX_QUEUE) {
    APP_LOG(APP_LOG_LEVEL_WARNING, "Outbox queue full, dropping cmd %d", cmd);
    return 0;
  }
  Outgoing *o = &s_queue[s_queue_len++];
  o->cmd = cmd;
  o->req = next_req();
  o->chat = chat;
  o->msg = msg;
  o->index = (int16_t)index;
  o->text = NULL;
  o->width = width;
  o->height = height;
  o->format = format;
  if (text) {
    size_t len = strlen(text) + 1;
    o->text = malloc(len);
    if (o->text) {
      memcpy(o->text, text, len);
    }
  }
  pump();
  return o->req;
}

static uint8_t enqueue(uint8_t cmd, uint16_t chat, uint16_t msg, int index, const char *text) {
  return enqueue_ex(cmd, chat, msg, index, text, 0, 0, 0);
}

// ---- Requests ----

void comms_get_status(void) {
  enqueue(CMD_GET_STATUS, 0, 0, -1, NULL);
}

void comms_pair(void) {
  enqueue(CMD_PAIR, 0, 0, -1, NULL);
}

void comms_get_chats(int page) {
  if (page == 0) {
    model_clear_chats();
  }
  g_model.chats_loading = true;
  s_chats_req = enqueue(CMD_GET_CHATS, 0, 0, page, NULL);
}

void comms_get_messages(uint16_t chat, uint16_t before) {
  if (!before) {
    model_clear_messages(chat);
  }
  g_model.messages_loading = true;
  s_msgs_older = before != 0;
  s_insert_at = 0;
  s_msgs_req = enqueue(CMD_GET_MESSAGES, chat, before, -1, NULL);
}

void comms_send_text(uint16_t chat, const char *text) {
  uint8_t req = enqueue(CMD_SEND_TEXT, chat, 0, -1, text);
  if (!req || chat != g_model.open_chat) {
    return;
  }
  Message m = { .handle = 0, .req = req, .ts = (int32_t)time(NULL),
                .flags = FLAG_FROM_ME | (MSG_STATUS_PENDING << FLAG_STATUS_SHIFT) };
  model_copy(m.text, text, sizeof(m.text));
  model_append_message(&m);
  conversation_window_refresh(true);
}

void comms_mark_read(uint16_t chat) {
  enqueue(CMD_MARK_READ, chat, 0, -1, NULL);
  Chat *c = model_find_chat(chat);
  if (c && c->unread) {
    c->unread = 0;
    chat_list_window_refresh();
  }
}

void comms_get_replies(void) {
  g_model.reply_count = 0;
  s_replies_req = enqueue(CMD_GET_REPLIES, 0, 0, -1, NULL);
}

uint8_t comms_get_full_text(uint16_t chat, uint16_t msg) {
  return enqueue(CMD_GET_FULL_TEXT, chat, msg, -1, NULL);
}

uint8_t comms_get_image(uint16_t chat, uint16_t msg, uint8_t width, uint8_t height, uint8_t format) {
  return enqueue_ex(CMD_GET_IMAGE, chat, msg, -1, NULL, width, height, format);
}

void comms_close_chat(uint16_t chat) {
  enqueue(CMD_CLOSE_CHAT, chat, 0, -1, NULL);
  g_model.open_chat = 0;
}

// ---- Inbox ----

static uint32_t get_uint(DictionaryIterator *iter, uint32_t key, uint32_t def) {
  Tuple *t = dict_find(iter, key);
  if (!t) {
    return def;
  }
  switch (t->length) {
    case 1: return t->type == TUPLE_INT ? (uint32_t)t->value->int8 : t->value->uint8;
    case 2: return t->type == TUPLE_INT ? (uint32_t)t->value->int16 : t->value->uint16;
    default: return t->type == TUPLE_INT ? (uint32_t)t->value->int32 : t->value->uint32;
  }
}

static const char *get_str(DictionaryIterator *iter, uint32_t key) {
  Tuple *t = dict_find(iter, key);
  return (t && t->type == TUPLE_CSTRING) ? t->value->cstring : "";
}

static void read_chat(DictionaryIterator *iter, Chat *c) {
  memset(c, 0, sizeof(*c));
  c->handle = (uint16_t)get_uint(iter, MESSAGE_KEY_CHAT, 0);
  model_copy(c->name, get_str(iter, MESSAGE_KEY_NAME), sizeof(c->name));
  model_copy(c->preview, get_str(iter, MESSAGE_KEY_TEXT), sizeof(c->preview));
  c->ts = (int32_t)get_uint(iter, MESSAGE_KEY_TS, 0);
  c->unread = (uint8_t)get_uint(iter, MESSAGE_KEY_UNREAD, 0);
  c->flags = (uint8_t)get_uint(iter, MESSAGE_KEY_FLAGS, 0);
}

static void read_message(DictionaryIterator *iter, Message *m) {
  memset(m, 0, sizeof(*m));
  m->handle = (uint16_t)get_uint(iter, MESSAGE_KEY_MSG, 0);
  model_copy(m->sender, get_str(iter, MESSAGE_KEY_NAME), sizeof(m->sender));
  model_copy(m->text, get_str(iter, MESSAGE_KEY_TEXT), sizeof(m->text));
  m->ts = (int32_t)get_uint(iter, MESSAGE_KEY_TS, 0);
  m->flags = (uint8_t)get_uint(iter, MESSAGE_KEY_FLAGS, 0);
}

// A live message from us may arrive before SEND_RESULT; adopt the pending copy instead of duplicating.
static Message *find_pending_match(const Message *m) {
  if (!(m->flags & FLAG_FROM_ME)) {
    return NULL;
  }
  for (int i = 0; i < g_model.message_count; i++) {
    Message *p = &g_model.messages[i];
    if (p->handle == 0 && !(p->flags & FLAG_FAILED) && strcmp(p->text, m->text) == 0) {
      return p;
    }
  }
  return NULL;
}

static void inbox_received(DictionaryIterator *iter, void *context) {
  uint8_t cmd = (uint8_t)get_uint(iter, MESSAGE_KEY_CMD, 0);
  uint8_t req = (uint8_t)get_uint(iter, MESSAGE_KEY_REQ, 0);
  uint8_t flags = (uint8_t)get_uint(iter, MESSAGE_KEY_FLAGS, 0);
  uint16_t chat = (uint16_t)get_uint(iter, MESSAGE_KEY_CHAT, 0);

  switch (cmd) {
    case CMD_STATUS:
      status_window_on_status((ServerState)get_uint(iter, MESSAGE_KEY_STATE, STATE_UNKNOWN),
                              (uint8_t)get_uint(iter, MESSAGE_KEY_ERR, ERR_OK),
                              get_str(iter, MESSAGE_KEY_TEXT));
      break;

    case CMD_CHAT_ITEM: {
      if (req != s_chats_req) {
        break;
      }
      Chat c;
      read_chat(iter, &c);
      model_upsert_chat(&c, false);
      chat_list_window_refresh();
      break;
    }
    case CMD_CHATS_END:
      if (req != s_chats_req) {
        break;
      }
      g_model.chats_loading = false;
      g_model.chats_more = (flags & FLAG_MORE) && g_model.chat_count < MAX_CHATS;
      g_model.chat_pages++;
      chat_list_window_refresh();
      break;
    case CMD_EVT_CHAT: {
      Chat c;
      read_chat(iter, &c);
      model_upsert_chat(&c, true);
      chat_list_window_refresh();
      break;
    }

    case CMD_MSG_ITEM: {
      if (req != s_msgs_req || chat != g_model.open_chat) {
        break;
      }
      Message m;
      read_message(iter, &m);
      if (s_msgs_older) {
        model_insert_message(s_insert_at++, &m);
      } else {
        model_append_message(&m);
      }
      break;
    }
    case CMD_MSGS_END:
      if (req != s_msgs_req || chat != g_model.open_chat) {
        break;
      }
      g_model.messages_loading = false;
      g_model.messages_more = flags & FLAG_MORE;
      if (!s_msgs_older) {
        g_model.messages_loaded = true;
        comms_mark_read(chat);
      }
      conversation_window_refresh(!s_msgs_older);
      break;
    case CMD_EVT_MESSAGE: {
      if (chat != g_model.open_chat) {
        break;
      }
      Message m;
      read_message(iter, &m);
      if (model_find_message(m.handle)) {
        break;
      }
      Message *pending = find_pending_match(&m);
      if (pending) {
        pending->handle = m.handle;
        pending->flags = m.flags;
      } else {
        model_append_message(&m);
        if (!(m.flags & FLAG_FROM_ME)) {
          comms_mark_read(chat);
        }
      }
      conversation_window_refresh(true);
      break;
    }

    case CMD_SEND_RESULT: {
      uint8_t err = (uint8_t)get_uint(iter, MESSAGE_KEY_ERR, ERR_OK);
      uint16_t handle = (uint16_t)get_uint(iter, MESSAGE_KEY_MSG, 0);
      for (int i = 0; i < g_model.message_count; i++) {
        Message *m = &g_model.messages[i];
        if (m->handle == 0 && m->req == req) {
          if (err == ERR_OK) {
            if (model_find_message(handle)) {
              // The live event already added it; drop our pending copy.
              memmove(m, m + 1, sizeof(Message) * (g_model.message_count - i - 1));
              g_model.message_count--;
            } else {
              m->handle = handle;
              m->flags = (m->flags & ~FLAG_STATUS_MASK) | (MSG_STATUS_SENT << FLAG_STATUS_SHIFT);
            }
          } else {
            m->flags |= FLAG_FAILED;
            vibes_double_pulse();
          }
          break;
        }
      }
      conversation_window_refresh(false);
      break;
    }

    case CMD_REPLY_ITEM:
      if (req == s_replies_req && g_model.reply_count < MAX_REPLIES) {
        model_copy(g_model.replies[g_model.reply_count++], get_str(iter, MESSAGE_KEY_TEXT), REPLY_LEN);
      }
      break;
    case CMD_REPLIES_END:
      if (req == s_replies_req) {
        g_model.replies_loaded = true;
        reply_menu_on_replies_loaded();
      }
      break;

    case CMD_TEXT_CHUNK: {
      Tuple *data = dict_find(iter, MESSAGE_KEY_DATA);
      message_window_on_text_chunk(req, get_uint(iter, MESSAGE_KEY_OFFSET, 0), get_uint(iter, MESSAGE_KEY_TOTAL, 0),
                                   data ? data->value->data : NULL, data ? data->length : 0);
      break;
    }
    case CMD_IMAGE_BEGIN:
      message_window_on_image_begin(req, (uint16_t)get_uint(iter, MESSAGE_KEY_WIDTH, 0),
                                    (uint16_t)get_uint(iter, MESSAGE_KEY_HEIGHT, 0),
                                    (uint8_t)get_uint(iter, MESSAGE_KEY_FORMAT, FORMAT_COLOR),
                                    (uint16_t)get_uint(iter, MESSAGE_KEY_STRIDE, 0),
                                    get_uint(iter, MESSAGE_KEY_TOTAL, 0));
      break;
    case CMD_IMAGE_CHUNK: {
      Tuple *data = dict_find(iter, MESSAGE_KEY_DATA);
      if (data) {
        message_window_on_image_chunk(req, get_uint(iter, MESSAGE_KEY_OFFSET, 0), data->value->data, data->length);
      }
      break;
    }
    case CMD_IMAGE_END:
      message_window_on_image_end(req);
      break;

    case CMD_ERROR: {
      uint8_t err = (uint8_t)get_uint(iter, MESSAGE_KEY_ERR, ERR_OTHER);
      const char *text = get_str(iter, MESSAGE_KEY_TEXT);
      if (message_window_owns_req(req)) {
        message_window_on_error(req, err, text);
      } else if (req == s_msgs_req && g_model.messages_loading) {
        g_model.messages_loading = false;
        conversation_window_on_error(err, text);
      } else if (req == s_chats_req && g_model.chats_loading) {
        g_model.chats_loading = false;
        chat_list_window_on_error(err, text);
      }
      if (err == ERR_UNREACHABLE || err == ERR_NOT_PAIRED || err == ERR_BAD_TOKEN) {
        comms_get_status();
      }
      break;
    }
    default:
      APP_LOG(APP_LOG_LEVEL_WARNING, "Unknown cmd %d", cmd);
  }
}

static void inbox_dropped(AppMessageResult reason, void *context) {
  APP_LOG(APP_LOG_LEVEL_WARNING, "Inbox dropped: %d", (int)reason);
}

void comms_init(void) {
  app_message_register_inbox_received(inbox_received);
  app_message_register_inbox_dropped(inbox_dropped);
  app_message_register_outbox_sent(outbox_sent);
  app_message_register_outbox_failed(outbox_failed);
  app_message_open(2048, 640);
}

void comms_deinit(void) {
  app_message_deregister_callbacks();
  for (int i = 0; i < s_queue_len; i++) {
    free(s_queue[i].text);
  }
  s_queue_len = 0;
}
