<p align="center">
<a href="https://roadrunner.dev" target="_blank">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="https://github.com/roadrunner-server/.github/assets/8040338/e6bde856-4ec6-4a52-bd5b-bfe78736c1ff">
    <img align="center" src="https://github.com/roadrunner-server/.github/assets/8040338/040fb694-1dd3-4865-9d29-8e0748c2c8b8">
  </picture>
</a>
</p>

# RoadRunner events bus

## Queued subscriptions

Use `SubscribePQueued` to retain matching events while a subscriber is busy:

```go
bus, id := events.NewEventBus()
commands := make(chan events.Event)
if err := bus.SubscribePQueued(id, "*.EventJOBSDriverCommand", commands); err != nil {
    return err
}
defer bus.Unsubscribe(id)
```

Each queued subscription delivers events in bus order. Its queue grows in memory with the number of pending events. Delivery to other subscribers continues while its receiver is busy.

`Unsubscribe` and `UnsubscribeP` discard events that remain in the subscription queue and wait for its delivery goroutine to stop. Close the receiver channel after unsubscribe returns. The queue exists for the lifetime of the subscription in the current process.

`SubscribeP` and `SubscribeAll` use non-blocking delivery. They can drop events when a receiver channel is full.
