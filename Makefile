SRC_PATH := rapidyenc
BUILD_PATH := rapidyenc/build

TARGET_NAME :=
ifeq ($(OS),Windows_NT)
	TARGET_NAME = windows
	ifeq ($(PROCESSOR_ARCHITECTURE),AMD64)
		TARGET_NAME := ${TARGET_NAME}/amd64
	endif
	ifeq ($(PROCESSOR_ARCHITECTURE),x86)
		TARGET_NAME := ${TARGET_NAME}/386
	endif
	ifeq ($(PROCESSOR_ARCHITECTURE),ARM64)
		TARGET_NAME := ${TARGET_NAME}/arm64
	endif
else
	UNAME_S := $(shell uname -s)
	ifeq ($(UNAME_S),Linux)
		TARGET_NAME = linux
		UNAME_M := $(shell uname -m)
		ifeq ($(UNAME_M),x86_64)
			TARGET_NAME := ${TARGET_NAME}/amd64
		endif
		ifneq ($(filter %86,$(UNAME_M)),)
			TARGET_NAME := ${TARGET_NAME}/386
		endif
		ifeq ($(UNAME_M),aarch64)
			TARGET_NAME := ${TARGET_NAME}/arm64
		endif
	endif
	ifeq ($(UNAME_S),Darwin)
		TARGET_NAME = darwin
	endif
endif

default: native

.PHONY: native
native: $(TARGET_NAME)

.PHONY: linux/amd64
linux/amd64:
	cmake -S ${SRC_PATH} -B ${BUILD_PATH} -DCMAKE_TOOLCHAIN_FILE=../../toolchain-linux-amd64.cmake
	cmake --build ${BUILD_PATH} --target rapidyenc_static -j $(shell nproc)
	cp ${BUILD_PATH}/rapidyenc_static/librapidyenc.a librapidyenc_linux_amd64.a

.PHONY: linux/arm64
linux/arm64:
	cmake -S ${SRC_PATH} -B ${BUILD_PATH} -DCMAKE_TOOLCHAIN_FILE=../../toolchain-linux-arm64.cmake
	cmake --build ${BUILD_PATH} --target rapidyenc_static -j $(shell nproc)
	cp ${BUILD_PATH}/rapidyenc_static/librapidyenc.a librapidyenc_linux_arm64.a

.PHONY: darwin
darwin:
	cmake -S ${SRC_PATH} -B ${BUILD_PATH}/amd64 -DCMAKE_TOOLCHAIN_FILE=${PWD}/toolchain-darwin-amd64.cmake
	ZERO_AR_DATE=1 cmake --build ${BUILD_PATH}/amd64 --target rapidyenc_static -j $(shell sysctl -n hw.ncpu)
	cmake -S ${SRC_PATH} -B ${BUILD_PATH}/arm64 -DCMAKE_TOOLCHAIN_FILE=${PWD}/toolchain-darwin-arm64.cmake
	ZERO_AR_DATE=1 cmake --build ${BUILD_PATH}/arm64 --target rapidyenc_static -j $(shell sysctl -n hw.ncpu)
	lipo -create -output librapidyenc_darwin.a ${BUILD_PATH}/amd64/rapidyenc_static/librapidyenc.a ${BUILD_PATH}/arm64/rapidyenc_static/librapidyenc.a

.PHONY: windows/amd64
windows/amd64:
	cmake -S ${SRC_PATH} -B ${BUILD_PATH} -G Ninja
	cmake --build ${BUILD_PATH} --target rapidyenc_static
	cp ${BUILD_PATH}/rapidyenc_static/librapidyenc.a librapidyenc_windows_amd64.a

.PHONY: update
update:
	git submodule update --recursive --remote
	cp rapidyenc/rapidyenc.h rapidyenc.h
	git add rapidyenc.h

.PHONY: clean
clean:
	@echo CLEAN $(BUILD_PATH)
	@rm -rf $(BUILD_PATH)
