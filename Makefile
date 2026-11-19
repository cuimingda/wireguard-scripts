export DESTDIR

.PHONY: help install

help:
	@printf '%s\n' 'Run make install to build and install wgk.'

install:
	bash ./lib/install-wgk.sh
