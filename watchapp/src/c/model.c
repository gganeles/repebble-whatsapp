#include "model.h"

Model g_model;

void model_copy(char *dest, const char *src, size_t size) {
  if (!src) {
    dest[0] = '\0';
    return;
  }
  strncpy(dest, src, size - 1);
  dest[size - 1] = '\0';
}

Chat *model_find_chat(uint16_t handle) {
  for (int i = 0; i < g_model.chat_count; i++) {
    if (g_model.chats[i].handle == handle) {
      return &g_model.chats[i];
    }
  }
  return NULL;
}

static void sort_chats(void) {
  // Insertion sort: the list is short and nearly sorted.
  for (int i = 1; i < g_model.chat_count; i++) {
    Chat tmp = g_model.chats[i];
    int j = i - 1;
    while (j >= 0 && g_model.chats[j].ts < tmp.ts) {
      g_model.chats[j + 1] = g_model.chats[j];
      j--;
    }
    g_model.chats[j + 1] = tmp;
  }
}

Chat *model_upsert_chat(const Chat *chat, bool resort) {
  Chat *existing = model_find_chat(chat->handle);
  if (!existing) {
    if (g_model.chat_count < MAX_CHATS) {
      existing = &g_model.chats[g_model.chat_count++];
    } else if (resort && chat->ts > g_model.chats[MAX_CHATS - 1].ts) {
      // Full: a live update for an unseen chat replaces the oldest one.
      existing = &g_model.chats[MAX_CHATS - 1];
    } else {
      return NULL;
    }
  }
  *existing = *chat;
  if (resort) {
    sort_chats();
    return model_find_chat(chat->handle);
  }
  return existing;
}

void model_clear_chats(void) {
  g_model.chat_count = 0;
  g_model.chats_more = false;
  g_model.chat_pages = 0;
}

void model_clear_messages(uint16_t chat) {
  g_model.open_chat = chat;
  g_model.message_count = 0;
  g_model.messages_more = false;
  g_model.messages_loading = false;
  g_model.messages_loaded = false;
}

Message *model_find_message(uint16_t handle) {
  if (handle == 0) {
    return NULL;
  }
  for (int i = 0; i < g_model.message_count; i++) {
    if (g_model.messages[i].handle == handle) {
      return &g_model.messages[i];
    }
  }
  return NULL;
}

Message *model_append_message(const Message *msg) {
  if (g_model.message_count == MAX_MESSAGES) {
    memmove(&g_model.messages[0], &g_model.messages[1], sizeof(Message) * (MAX_MESSAGES - 1));
    g_model.message_count--;
    g_model.messages_more = true;
  }
  Message *slot = &g_model.messages[g_model.message_count++];
  *slot = *msg;
  return slot;
}

Message *model_insert_message(int index, const Message *msg) {
  if (index < 0 || index > g_model.message_count || index >= MAX_MESSAGES) {
    return NULL;
  }
  int count = g_model.message_count;
  if (count == MAX_MESSAGES) {
    count--;  // drop the newest
  }
  memmove(&g_model.messages[index + 1], &g_model.messages[index], sizeof(Message) * (count - index));
  g_model.messages[index] = *msg;
  g_model.message_count = count + 1;
  return &g_model.messages[index];
}
