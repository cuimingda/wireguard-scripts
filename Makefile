export DESTDIR

.PHONY: help install

help:
	@printf '%s\n' 'Run make install to build and install wgt.'

install:
	bash ./lib/install-wgt.sh
