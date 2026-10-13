import Foundation
import Darwin

@main
struct ProtocolSmoke {
 static func main() throws {
  let path = CommandLine.arguments[1]
  let fd = Darwin.socket(AF_UNIX, SOCK_STREAM, 0)
  guard fd >= 0 else { throw POSIXError(.EIO) }
  let file = FileHandle(fileDescriptor: fd, closeOnDealloc: true)
  defer { try? file.close() }
  var address = sockaddr_un()
  address.sun_family = sa_family_t(AF_UNIX)
  address.sun_len = UInt8(MemoryLayout<sockaddr_un>.size)
  let bytes = Array(path.utf8) + [0]
  guard bytes.count <= MemoryLayout.size(ofValue: address.sun_path) else { throw POSIXError(.ENAMETOOLONG) }
  withUnsafeMutableBytes(of: &address.sun_path) { buffer in buffer.copyBytes(from: bytes) }
  let connected = withUnsafePointer(to: &address) { pointer in
   pointer.withMemoryRebound(to: sockaddr.self, capacity: 1) {
    Darwin.connect(fd, $0, socklen_t(MemoryLayout<sockaddr_un>.size))
   }
  }
  guard connected == 0 else { throw POSIXError(.ECONNREFUSED) }
  func call(_ id: Int, _ method: String, _ params: JSONValue) throws -> Message {
   let request = Message(jsonrpc: "2.0", id: .number(Double(id)), method: method, params: params, result: nil, error: nil)
   var data = try JSONEncoder().encode(request)
   data.append(10)
   try file.write(contentsOf: data)
   var response = Data()
   while true {
    guard let byte = try file.read(upToCount: 1), !byte.isEmpty else { throw POSIXError(.EIO) }
    if byte[0] == 10 { break }
    response.append(byte)
   }
   let message = try JSONDecoder().decode(Message.self, from: response)
   guard message.error == nil else { throw POSIXError(.EPROTO) }
   return message
  }
  let initialized = try call(1, "initialize", .object([
   "client_name": .string("romp-app-fixture"), "client_version": .string("v0.1.0"),
   "protocol_min": .number(1), "protocol_max": .number(1), "capabilities": .array([])
  ]))
  let info = try JSONDecoder().decode(Initialized.self, from: JSONEncoder().encode(initialized.result))
  guard info.protocolVersion == 1 else { throw POSIXError(.EPROTO) }
  let reply = try call(2, "daemon.health", .object([:]))
  let health = try JSONDecoder().decode(Health.self, from: JSONEncoder().encode(reply.result))
  guard health.restartReady else { throw POSIXError(.EBUSY) }
  print("Swift protocol compatible with \(info.daemonVersion)")
 }
}
