#pragma once
#include <pebble.h>

#define THEME_ACCENT      PBL_IF_COLOR_ELSE(GColorIslamicGreen, GColorBlack)
#define THEME_ON_ACCENT   GColorWhite
#define THEME_BG          PBL_IF_COLOR_ELSE(GColorWhite, GColorWhite)
#define THEME_CHAT_BG     PBL_IF_COLOR_ELSE(GColorLightGray, GColorWhite)
#define THEME_BUBBLE_ME   PBL_IF_COLOR_ELSE(GColorMintGreen, GColorWhite)
#define THEME_BUBBLE_THEM GColorWhite
#define THEME_SUBTLE      PBL_IF_COLOR_ELSE(GColorDarkGray, GColorBlack)
#define THEME_BADGE       PBL_IF_COLOR_ELSE(GColorIslamicGreen, GColorBlack)
#define THEME_ERROR       PBL_IF_COLOR_ELSE(GColorDarkCandyAppleRed, GColorBlack)

#define HEADER_HEIGHT 24
