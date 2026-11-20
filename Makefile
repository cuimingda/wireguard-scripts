GO ?= /usr/lib/go-1.26/bin/go
export GO DESTDIR

.PHONY: help install

help:
	@printf '%s\n' 'Run ./setup.sh for the first install.' 'Run make install to rebuild and reinstall wgk.'

install:
	bash ./lib/wgk-install.sh
