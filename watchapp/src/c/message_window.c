// One message in full: its picture (if any) and the complete text, scrollable.
// Select opens the reply menu.
#include <pebble.h>
#include "comms.h"
#include "model.h"
#include "theme.h"
#include "ui.h"

#define PAD 4
#define MAX_CONTENT_HEIGHT 6000
// Leave this much heap free after allocating the picture.
#define HEAP_RESERVE 6000

static Window *s_window;
static ScrollLayer *s_scroll;
static TextLayer *s_header;
static TextLayer *s_image_note;  // "Loading picture..." / errors
static BitmapLayer *s_image_layer;
static TextLayer *s_body;
static TextLayer *s_footer;

static uint16_t s_chat;
static Message s_msg;            // snapshot of the message as listed
static char s_header_text[CHAT_NAME_LEN];
static char s_footer_text[48];
static char s_image_note_text[48];

static char *s_full_text;        // assembled from TEXT_CHUNKs
static uint32_t s_full_total;
static bool s_full_loaded;
static uint8_t s_text_req;

static GBitmap *s_bitmap;
static uint8_t *s_bitmap_data;
static uint16_t s_bitmap_stride;
static uint16_t s_src_stride;
static uint32_t s_image_total;
static bool s_image_ready;
static uint8_t s_image_req;

static bool wants_image(void) {
  return s_msg.flags & FLAG_IMAGE;
}

static void relayout(void) {
  if (!s_window) {
    return;
  }
  GRect b = layer_get_bounds(window_get_root_layer(s_window));
  int w = b.size.w - 2 * PAD;
  int y = PAD;

  text_layer_set_text(s_image_note, s_image_note_text);
  layer_set_hidden(bitmap_layer_get_layer(s_image_layer), !s_image_ready);
  layer_set_hidden(text_layer_get_layer(s_image_note), !wants_image() || s_image_ready);
  if (wants_image()) {
    if (s_image_ready) {
      GRect ib = gbitmap_get_bounds(s_bitmap);
      layer_set_frame(bitmap_layer_get_layer(s_image_layer), GRect(0, y, b.size.w, ib.size.h));
      y += ib.size.h + PAD;
    } else {
      layer_set_frame(text_layer_get_layer(s_image_note), GRect(PAD, y, w, 24));
      y += 24 + PAD;
    }
  }

  text_layer_set_text(s_body, s_full_loaded ? s_full_text : s_msg.text);
  layer_set_frame(text_layer_get_layer(s_body), GRect(PAD, y, w, MAX_CONTENT_HEIGHT));
  GSize body = text_layer_get_content_size(s_body);
  layer_set_frame(text_layer_get_layer(s_body), GRect(PAD, y, w, body.h + 6));
  y += body.h + 6;

  char when[12];
  format_time(s_msg.ts, when, sizeof(when));
  bool loading_text = (s_msg.flags & FLAG_TRUNCATED) && !s_full_loaded;
  snprintf(s_footer_text, sizeof(s_footer_text), "%s%s%s", s_msg.sender[0] ? s_msg.sender : "",
           s_msg.sender[0] ? ", " : "", loading_text ? "loading the rest..." : when);
  text_layer_set_text(s_footer, s_footer_text);
  layer_set_frame(text_layer_get_layer(s_footer), GRect(PAD, y, w, 20));
  y += 20 + PAD;

  scroll_layer_set_content_size(s_scroll, GSize(b.size.w, y));
}

static void free_image(void) {
  if (s_bitmap) {
    gbitmap_destroy(s_bitmap);
    s_bitmap = NULL;
  }
  s_bitmap_data = NULL;
  s_image_ready = false;
}

// Picks the largest picture size that fits the screen width and the free heap.
static void request_image(GRect bounds) {
  int side = bounds.size.w - 2 * PAD;
#if defined(PBL_COLOR)
  uint8_t format = FORMAT_COLOR;
  int bytes_per_px_x8 = 8;
#else
  uint8_t format = FORMAT_BW;
  int bytes_per_px_x8 = 1;
#endif
  while (side > 48 && (int)heap_bytes_free() < (side * side * bytes_per_px_x8) / 8 + HEAP_RESERVE) {
    side = side * 3 / 4;
  }
  if (side > 255) {
    side = 255;
  }
  snprintf(s_image_note_text, sizeof(s_image_note_text), "Loading picture...");
  s_image_req = comms_get_image(s_chat, s_msg.handle, (uint8_t)side, (uint8_t)side, format);
}

static void select_click(ClickRecognizerRef recognizer, void *context) {
  reply_menu_open(s_chat);
}

static void click_config(void *context) {
  window_single_click_subscribe(BUTTON_ID_SELECT, select_click);
}

static void window_load(Window *window) {
  Layer *root = window_get_root_layer(window);
  GRect b = layer_get_bounds(root);
  window_set_background_color(window, THEME_BG);

  Chat *c = model_find_chat(s_chat);
  model_copy(s_header_text, (s_msg.flags & FLAG_FROM_ME) ? "You" : (s_msg.sender[0] ? s_msg.sender : (c ? c->name : "")),
             sizeof(s_header_text));
  s_header = text_layer_create(GRect(0, 0, b.size.w, HEADER_HEIGHT));
  text_layer_set_background_color(s_header, THEME_ACCENT);
  text_layer_set_text_color(s_header, THEME_ON_ACCENT);
  text_layer_set_font(s_header, fonts_get_system_font(FONT_KEY_GOTHIC_18_BOLD));
  text_layer_set_text_alignment(s_header, GTextAlignmentCenter);
  text_layer_set_overflow_mode(s_header, GTextOverflowModeTrailingEllipsis);
  text_layer_set_text(s_header, s_header_text);
  layer_add_child(root, text_layer_get_layer(s_header));

  GRect scroll_frame = GRect(0, HEADER_HEIGHT, b.size.w, b.size.h - HEADER_HEIGHT);
  s_scroll = scroll_layer_create(scroll_frame);
  scroll_layer_set_click_config_onto_window(s_scroll, window);
  scroll_layer_set_callbacks(s_scroll, (ScrollLayerCallbacks) { .click_config_provider = click_config });
  layer_add_child(root, scroll_layer_get_layer(s_scroll));

  s_image_layer = bitmap_layer_create(GRect(0, 0, b.size.w, 1));
  bitmap_layer_set_alignment(s_image_layer, GAlignCenter);
  bitmap_layer_set_compositing_mode(s_image_layer, GCompOpSet);
  scroll_layer_add_child(s_scroll, bitmap_layer_get_layer(s_image_layer));

  s_image_note = text_layer_create(GRect(0, 0, b.size.w, 24));
  text_layer_set_font(s_image_note, fonts_get_system_font(FONT_KEY_GOTHIC_18_BOLD));
  text_layer_set_text_color(s_image_note, THEME_SUBTLE);
  text_layer_set_text(s_image_note, s_image_note_text);
  scroll_layer_add_child(s_scroll, text_layer_get_layer(s_image_note));

  s_body = text_layer_create(GRect(0, 0, b.size.w, 20));
  text_layer_set_font(s_body, fonts_get_system_font(FONT_KEY_GOTHIC_24));
  text_layer_set_overflow_mode(s_body, GTextOverflowModeWordWrap);
  scroll_layer_add_child(s_scroll, text_layer_get_layer(s_body));

  s_footer = text_layer_create(GRect(0, 0, b.size.w, 20));
  text_layer_set_font(s_footer, fonts_get_system_font(FONT_KEY_GOTHIC_14));
  text_layer_set_text_color(s_footer, THEME_SUBTLE);
  scroll_layer_add_child(s_scroll, text_layer_get_layer(s_footer));

  s_full_loaded = false;
  s_text_req = 0;
  s_image_req = 0;
  if (s_msg.flags & FLAG_TRUNCATED) {
    s_text_req = comms_get_full_text(s_chat, s_msg.handle);
  }
  if (wants_image()) {
    request_image(scroll_frame);
  }
  relayout();
}

static void window_unload(Window *window) {
  text_layer_destroy(s_header);
  text_layer_destroy(s_image_note);
  bitmap_layer_destroy(s_image_layer);
  text_layer_destroy(s_body);
  text_layer_destroy(s_footer);
  scroll_layer_destroy(s_scroll);
  free_image();
  free(s_full_text);
  s_full_text = NULL;
  s_text_req = s_image_req = 0;
  window_destroy(s_window);
  s_window = NULL;
}

void message_window_push(uint16_t chat, const Message *msg) {
  if (s_window || !msg || msg->handle == 0) {
    return;
  }
  s_chat = chat;
  s_msg = *msg;
  s_window = window_create();
  window_set_window_handlers(s_window, (WindowHandlers) {
    .load = window_load,
    .unload = window_unload,
  });
  window_stack_push(s_window, true);
}

bool message_window_owns_req(uint8_t req) {
  return s_window && req && (req == s_text_req || req == s_image_req);
}

void message_window_on_text_chunk(uint8_t req, uint32_t offset, uint32_t total, const uint8_t *data, uint16_t len) {
  if (!s_window || req != s_text_req) {
    return;
  }
  if (offset == 0) {
    free(s_full_text);
    s_full_total = total;
    s_full_text = malloc(total + 1);
    if (!s_full_text) {
      APP_LOG(APP_LOG_LEVEL_WARNING, "No memory for %d bytes of text", (int)total);
      s_text_req = 0;
      return;
    }
    s_full_text[total] = '\0';
  }
  if (!s_full_text || offset + len > s_full_total) {
    return;
  }
  if (data && len) {
    memcpy(s_full_text + offset, data, len);
  }
  if (offset + len >= s_full_total) {
    s_full_loaded = true;
    s_text_req = 0;
    relayout();
  }
}

void message_window_on_image_begin(uint8_t req, uint16_t width, uint16_t height, uint8_t format, uint16_t stride, uint32_t total) {
  if (!s_window || req != s_image_req) {
    return;
  }
  free_image();
  GBitmapFormat fmt = format == FORMAT_BW ? GBitmapFormat1Bit : GBitmapFormat8Bit;
  s_bitmap = gbitmap_create_blank(GSize(width, height), fmt);
  if (!s_bitmap) {
    snprintf(s_image_note_text, sizeof(s_image_note_text), "Picture too big for the watch");
    s_image_req = 0;
    relayout();
    return;
  }
  s_bitmap_data = gbitmap_get_data(s_bitmap);
  s_bitmap_stride = gbitmap_get_bytes_per_row(s_bitmap);
  s_src_stride = stride;
  s_image_total = total;
}

// Chunks are tightly packed rows; the bitmap may pad each row, so copy row by row.
void message_window_on_image_chunk(uint8_t req, uint32_t offset, const uint8_t *data, uint16_t len) {
  if (!s_window || req != s_image_req || !s_bitmap_data || !s_src_stride) {
    return;
  }
  GRect ib = gbitmap_get_bounds(s_bitmap);
  uint32_t end = offset + len;
  if (end > s_image_total) {
    end = s_image_total;
  }
  for (uint32_t pos = offset; pos < end;) {
    uint32_t row = pos / s_src_stride;
    uint32_t col = pos % s_src_stride;
    uint32_t n = s_src_stride - col;
    if (n > end - pos) {
      n = end - pos;
    }
    if (row >= (uint32_t)ib.size.h) {
      break;
    }
    memcpy(s_bitmap_data + row * s_bitmap_stride + col, data + (pos - offset), n);
    pos += n;
  }
}

void message_window_on_image_end(uint8_t req) {
  if (!s_window || req != s_image_req || !s_bitmap) {
    return;
  }
  s_image_req = 0;
  s_image_ready = true;
  bitmap_layer_set_bitmap(s_image_layer, s_bitmap);
  relayout();
}

void message_window_on_error(uint8_t req, uint8_t err, const char *text) {
  if (!s_window) {
    return;
  }
  if (req == s_image_req) {
    s_image_req = 0;
    free_image();
    snprintf(s_image_note_text, sizeof(s_image_note_text), "Picture unavailable");
  }
  if (req == s_text_req) {
    s_text_req = 0;
    // Keep showing the shortened text.
    s_msg.flags &= ~FLAG_TRUNCATED;
  }
  relayout();
}
