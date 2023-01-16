include(CheckCXXCompilerFlag)
cmake_minimum_required (VERSION 3.24)

project(yencode)

set(CMAKE_CXX_STANDARD 11)
set(CMAKE_MACOSX_RPATH OFF)
set(CMAKE_SHARED_LIBRARY_SUFFIX ".syso")

# if (${CMAKE_SYSTEM_PROCESSOR} MATCHES "i386|i686|x86|x86_64|x64|amd64")
#     set(IS_X86 TRUE)
# endif()
# if (${CMAKE_SYSTEM_PROCESSOR} MATCHES "arm|aarch64")
#     set(IS_ARM TRUE)
# endif()
# if (${CMAKE_SYSTEM_NAME} MATCHES "Darwin")
#     set(IS_MACOSX TRUE)
# endif()

# CHECK_CXX_COMPILER_FLAG("-mvpclmulqdq" COMPILER_SUPPORTS_VPCLMULQDQ)
# if(IS_X86 AND COMPILER_SUPPORTS_VPCLMULQDQ)
#     set(VPCLMULQDQ_FLAGS -mavx2 -mvpclmulqdq -mpclmul)
# endif()
# CHECK_CXX_COMPILER_FLAG("-mavx512vbmi2" COMPILER_SUPPORTS_AVX512VBMI2)
# if(IS_X86 AND COMPILER_SUPPORTS_AVX512VBMI2)
#     set(AVX512VBMI2_FLAGS -mavx512vbmi2 -mavx512vl -mavx512bw -mpopcnt -mbmi -mbmi2 -mlzcnt)
# endif()

set(NODE_YENCODE_LOCATION yencode)
set(CRCUTIL_LOCATION ${NODE_YENCODE_LOCATION}/crcutil-1.0)
set(YENCODE_SOURCES 
    ${NODE_YENCODE_LOCATION}/src/platform.cc 
    ${NODE_YENCODE_LOCATION}/src/encoder.cc 
    ${NODE_YENCODE_LOCATION}/src/decoder.cc 
    ${NODE_YENCODE_LOCATION}/src/crc.cc
)
set(CRCUTIL_SOURCES 
    ${CRCUTIL_LOCATION}/code/crc32c_sse4.cc 
    ${CRCUTIL_LOCATION}/code/multiword_64_64_cl_i386_mmx.cc 
    ${CRCUTIL_LOCATION}/code/multiword_64_64_gcc_amd64_asm.cc 
    ${CRCUTIL_LOCATION}/code/multiword_64_64_gcc_i386_mmx.cc 
    ${CRCUTIL_LOCATION}/code/multiword_64_64_intrinsic_i386_mmx.cc 
    ${CRCUTIL_LOCATION}/code/multiword_128_64_gcc_amd64_sse2.cc 
    ${CRCUTIL_LOCATION}/examples/interface.cc
)
set(SOURCES
    ${YENCODE_SOURCES}
    ${CRCUTIL_SOURCES}
    ${NODE_YENCODE_LOCATION}/src/crc_folding.cc
    ${NODE_YENCODE_LOCATION}/src/crc_folding_256.cc
    ${NODE_YENCODE_LOCATION}/src/encoder_sse2.cc  ${NODE_YENCODE_LOCATION}/src/decoder_sse2.cc
    ${NODE_YENCODE_LOCATION}/src/encoder_ssse3.cc ${NODE_YENCODE_LOCATION}/src/decoder_ssse3.cc
    ${NODE_YENCODE_LOCATION}/src/encoder_avx.cc   ${NODE_YENCODE_LOCATION}/src/decoder_avx.cc
    ${NODE_YENCODE_LOCATION}/src/encoder_avx2.cc  ${NODE_YENCODE_LOCATION}/src/decoder_avx2.cc
    ${NODE_YENCODE_LOCATION}/src/encoder_vbmi2.cc ${NODE_YENCODE_LOCATION}/src/decoder_vbmi2.cc
)

include_directories(${NODE_YENCODE_LOCATION}/crcutil-1.0/code ${NODE_YENCODE_LOCATION}/crcutil-1.0/examples ${NODE_YENCODE_LOCATION}/crcutil-1.0/tests)

add_library(yencode SHARED ${SOURCES})
target_compile_options(yencode PUBLIC -march=native -O3 -fPIC -fwrapv -mstackrealign -fno-asynchronous-unwind-tables -fno-jump-tables -mno-red-zone)
target_compile_definitions(yencode PUBLIC CRCUTIL_USE_MM_CRC32=0)
