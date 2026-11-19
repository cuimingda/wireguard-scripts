export DESTDIR

.PHONY: help install

help:
	@printf '%s\n' 'Run ./install-wgk.sh or make install to build and install wgk.'

install:
	./install-wgk.sh
