import Foundation
import UserNotifications
// Probe only: reads the notification settings of this bundle. It posts
// nothing and asks for no permission.
let sem = DispatchSemaphore(value: 0)
print("bundle:", Bundle.main.bundleIdentifier ?? "nil")
UNUserNotificationCenter.current().getNotificationSettings { s in
    print("authorizationStatus:", s.authorizationStatus.rawValue, "alertSetting:", s.alertSetting.rawValue)
    sem.signal()
}
if sem.wait(timeout: .now() + 5) == .timedOut { print("timeout"); exit(2) }
