#include <pebble.h>
#include "comms.h"
#include "model.h"
#include "touch.h"
#include "ui.h"

void format_time(int32_t ts, char *buf, size_t size) {
  time_t now = time(NULL);
  time_t t = (time_t)ts;
  int32_t diff = (int32_t)(now - t);
  if (ts <= 0) {
    buf[0] = '\0';
  } else if (diff < 60) {
    snprintf(buf, size, "now");
  } else if (diff < 60 * 60) {
    snprintf(buf, size, "%dm", (int)(diff / 60));
  } else if (diff < 24 * 60 * 60) {
    struct tm *tm = localtime(&t);
    strftime(buf, size, clock_is_24h_style() ? "%H:%M" : "%l:%M%p", tm);
  } else if (diff < 6 * 24 * 60 * 60) {
    strftime(buf, size, "%a", localtime(&t));
  } else {
    strftime(buf, size, "%d/%m", localtime(&t));
  }
}

static void init(void) {
  memset(&g_model, 0, sizeof(g_model));
  comms_init();
  touch_init();
  status_window_push();
  comms_get_status();
}

static void deinit(void) {
  touch_deinit();
  comms_deinit();
}

int main(void) {
  init();
  app_event_loop();
  deinit();
}
