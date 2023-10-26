cmake -S rapidyenc -B rapidyenc/build -DCMAKE_CXX_FLAGS=-msse4 -G "MinGW Makefiles"
cmake --build rapidyenc/build --config Release
copy "rapidyenc\build\rapidyenc_static\librapidyenc.a" "lib\librapidyenc_windows_amd64.a"