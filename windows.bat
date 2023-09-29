cd rapidyenc
cmake .
cmake --build . --config Release --target rapidyenc_shared
copy "Release\rapidyenc.dll" "..\lib"