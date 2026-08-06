@echo off
REM helm-x build script — MinGW static link
setlocal

cd /d %~dp0

REM 1. generate embedded resources
python tools\embed.py || goto :error

REM 2. configure + build
cmake -B build -G "MinGW Makefiles" -DCMAKE_BUILD_TYPE=Release || goto :error
cmake --build build -j || goto :error

echo.
echo [OK] build\helmx.exe
echo [OK] deps check: objdump -p build\helmx.exe ^| findstr "DLL Name"
goto :eof

:error
echo [FAIL] build error
exit /b 1
