#Use Vulcanize 1.0
VULCANIZE1=true
# The list of files we want to go into core.js, which is concat'd and
# minified. These files should be either present in the project, brought
# into third_party/bower_compoents via bower, or in node_modules.
CORE_SOURCE_FILES = third_party/bower_components/webcomponentsjs/webcomponents.min.js \
          ../res/js/common.js \
          res/js/status_utils.js

BOWER_DIR=third_party/bower_components

all: clean_webtools status

include ../go/skiaversion/skiaversion.mk

status: web skiaversion
	go install -v ./go/status

# Build the web assets in production mode.
.PHONY: web
web: touch_elements core_js elements_html
	ln -sf ../../res res/imp/res

.PHONY: touch_elements
touch_elements:
	rm -f res/js/core.js
	touch elements.html

testgo: skiaversion
	go test ./go/... -v

release: all
	./build_release "$(MESSAGE)"

# Build debug versions of core.js and elements.html.
.PHONY: debug
debug: clean_webtools debug_core_js debug_elements_html

include ../webtools/webtools.mk
