all:
	g++ -march=native -O3 -fPIC -fwrapv -std=c++11 \
	-DCRCUTIL_USE_MM_CRC32=0 \
	-Icrcutil-1.0/code \
	-Icrcutil-1.0/examples \
	-Icrcutil-1.0/tests \
	-c \
	src/platform.cc \
	src/encoder.cc \
	src/decoder.cc \
	src/crc.cc \
	crcutil-1.0/code/crc32c_sse4.cc \
	crcutil-1.0/code/multiword_64_64_cl_i386_mmx.cc \
	crcutil-1.0/code/multiword_64_64_gcc_amd64_asm.cc \
	crcutil-1.0/code/multiword_64_64_gcc_i386_mmx.cc \
	crcutil-1.0/code/multiword_64_64_intrinsic_i386_mmx.cc \
	crcutil-1.0/code/multiword_128_64_gcc_amd64_sse2.cc \
	crcutil-1.0/examples/interface.cc \
	src/crc_folding.cc \
	src/crc_folding_256.cc \
	src/encoder_sse2.cc \
	src/decoder_sse2.cc \
	src/encoder_ssse3.cc \
	src/decoder_ssse3.cc \
	src/encoder_avx.cc \
	src/decoder_avx.cc \
	src/encoder_avx2.cc \
	src/decoder_avx2.cc \
	src/encoder_vbmi2.cc \
	src/decoder_vbmi2.cc
	g++ -shared -o libyencode.syso \
	platform.o encoder.o decoder.o crc.o \
	encoder_sse2.o decoder_sse2.o \
	encoder_ssse3.o decoder_ssse3.o \
	crc_folding.o crc_folding_256.o \
	encoder_avx.o decoder_avx.o \
	encoder_avx2.o decoder_avx2.o \
	encoder_vbmi2.o decoder_vbmi2.o \
	crc32c_sse4.o multiword_64_64_cl_i386_mmx.o multiword_64_64_gcc_amd64_asm.o multiword_64_64_gcc_i386_mmx.o multiword_64_64_intrinsic_i386_mmx.o multiword_128_64_gcc_amd64_sse2.o interface.o
	mv -f libyencode.syso ..
	rm -f libyencode.o
