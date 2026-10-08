#pragma once
#include <pebble.h>

void comms_init(void);
void comms_deinit(void);

void comms_get_status(void);
void comms_pair(void);
void comms_get_chats(int page);
// before = message handle to load older than, or 0 for the newest page.
void comms_get_messages(uint16_t chat, uint16_t before);
// Adds an optimistic message to the open conversation and sends it.
void comms_send_text(uint16_t chat, const char *text);
void comms_mark_read(uint16_t chat);
void comms_get_replies(void);
void comms_close_chat(uint16_t chat);
// Detail view requests; return the request id (0 if the outbox is full).
uint8_t comms_get_full_text(uint16_t chat, uint16_t msg);
uint8_t comms_get_image(uint16_t chat, uint16_t msg, uint8_t width, uint8_t height, uint8_t format);
