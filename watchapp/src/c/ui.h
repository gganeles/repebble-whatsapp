#pragma once
#include <pebble.h>
#include "model.h"
#include "protocol.h"

// Called by comms when data arrives. Implemented by the windows.
void status_window_on_status(ServerState state, uint8_t err, const char *text);
void chat_list_window_refresh(void);
void chat_list_window_on_error(uint8_t err, const char *text);
void conversation_window_refresh(bool scroll_to_newest);
void conversation_window_on_error(uint8_t err, const char *text);
void reply_menu_on_replies_loaded(void);

// Message detail view (full text and picture), fed by chunked transfers.
bool message_window_owns_req(uint8_t req);
void message_window_on_text_chunk(uint8_t req, uint32_t offset, uint32_t total, const uint8_t *data, uint16_t len);
void message_window_on_image_begin(uint8_t req, uint16_t width, uint16_t height, uint8_t format, uint16_t stride, uint32_t total);
void message_window_on_image_chunk(uint8_t req, uint32_t offset, const uint8_t *data, uint16_t len);
void message_window_on_image_end(uint8_t req);
void message_window_on_error(uint8_t req, uint8_t err, const char *text);

// Window stack helpers.
void status_window_push(void);
void chat_list_window_push(void);
bool chat_list_window_is_loaded(void);
void conversation_window_push(uint16_t chat);
void reply_menu_open(uint16_t chat);
void message_window_push(uint16_t chat, const Message *msg);

// Short relative time like "now", "5m", "3h", "Tue" or "12/31".
void format_time(int32_t ts, char *buf, size_t size);
