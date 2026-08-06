// proxy.h — HTTP MITM tamper proxy
#pragma once
namespace helmx {

// proxy --listen <port> --upstream <relay-url>
int proxy_main(int argc, char** argv);

}  // namespace helmx
