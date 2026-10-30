import Foundation
import UserNotifications
// Prototype of the kjak macOS notification helper. It reads one
// notification as JSON on stdin ({"title","subtitle","body"}), asks for
// permission the first time, posts it and exits:
//   0 posted (accepted by the notification center)
//   3 notifications not allowed (denied, or alerts off)
//   4 the notification center refused it (message on stderr)
//   2 bad input, 5 no answer in time
struct N: Decodable { let title: String; let subtitle: String; let body: String }
func fail(_ code: Int32, _ msg: String) -> Never {
    FileHandle.standardError.write((msg + "\n").data(using: .utf8)!); exit(code)
}
guard let n = try? JSONDecoder().decode(N.self, from: FileHandle.standardInput.readDataToEndOfFile()) else { fail(2, "bad input") }
let c = UNUserNotificationCenter.current()
let done = DispatchSemaphore(value: 0)
var status: Int32 = 5
var message = "no answer from the notification center"
c.requestAuthorization(options: [.alert]) { granted, err in
    c.getNotificationSettings { s in
        if !granted || s.authorizationStatus == .denied || s.alertSetting == .disabled {
            status = 3; message = "notifications are not allowed for kjak (System Settings, Notifications)"
            done.signal(); return
        }
        let content = UNMutableNotificationContent()
        content.title = n.title; content.subtitle = n.subtitle; content.body = n.body
        c.add(UNNotificationRequest(identifier: UUID().uuidString, content: content, trigger: nil)) { e in
            if let e = e { status = 4; message = e.localizedDescription } else { status = 0; message = "" }
            done.signal()
        }
    }
}
if done.wait(timeout: .now() + 60) == .timedOut { fail(5, message) }
if status != 0 { fail(status, message) }
exit(0)
