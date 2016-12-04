all : ducko.exe

ducko.exe : ducko.go Makefile ducko.syso
	go build -ldflags="-H windowsgui"

ducko.syso : ducko.rc
	windres ducko.rc ducko.syso

clean :
	-rm *.syso *.exe
