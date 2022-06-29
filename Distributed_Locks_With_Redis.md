## Distributed Locks with Redis

## 用 Redis 来实现分布式锁

### A Distributed Lock Pattern with Redis

### 一个基于 Redis 的分布式锁设计模式

Distributed locks are a very useful primitive in many environments where different processes must operate with shared resources in a mutually exclusive way.
分布式锁在那些需要对资源进行独占访问的场景里起着至关重要的作用

There are a number of libraries and blog posts describing how to implement a DLM (Distributed Lock Manager) with Redis, but every library uses a different approach, and many use a simple approach with lower guarantees compared to what can be achieved with slightly more complex designs.
现在已经有很多的开源框架和文章来告诉我们该如何利用 Redis 实现一个分布式锁管理器 DLM(Distributed Lock Manager), 但是实现的方式五花八门，而且大部分使用的方法都比较简单， 无法保证可靠性， 其实我们只需要采用稍微复杂一点的设计就可以大幅提高可靠性

This page describes a more canonical algorithm to implement distributed locks with Redis. We propose an algorithm, called Redlock, which implements a DLM which we believe to be safer than the vanilla single instance approach. We hope that the community will analyze it, provide feedback, and use it as a starting point for the implementations or more complex or alternative designs.
现在我们将用一个公认更加成熟的算法来实现一个分布式锁。 我们提出了一个算法， 我们称它为"Redlock", 它实现了一个比 vanilla 单点方法更安全的分布式锁管理器。 我们希望大家能测试一下它， 给我们提供反馈， 并且以此为基础来实现更复杂的设计模式。

### Safety and Liveness Guarantees

### 安全性和系统活力保证

We are going to model our design with just three properties that, from our point of view, are the minimum guarantees needed to use distributed locks in an effective way.
我们将我们的设计总结出三个属性， 在我们看来， 这些是分布式锁系统有效运转的最低保证。

Safety property: Mutual exclusion. At any given moment, only one client can hold a lock.
安全: 任何时间点，只有一个客户端可以持针对某一特定资源的锁

Liveness property A: Deadlock free. Eventually it is always possible to acquire a lock, even if the client that locked a resource crashes or gets partitioned.
活力 A: 无死锁。 任何客户端在任何时候都可以获取锁， 即使某些持有锁的客户端挂掉或者出现网络隔离

Liveness property B: Fault tolerance. As long as the majority of Redis nodes are up, clients are able to acquire and release locks.
活力 B: 容错， 只要超过半数的 Redis 节点存活， 客户端就可以获取和释放锁

### Why Failover-based Implementations Are Not Enough

### 为什么主备形式的实现方式不能满足需求?

To understand what we want to improve, let’s analyze the current state of affairs with most Redis-based distributed lock libraries.
为了让大家明白我们改进了什么， 让我们来分析一下当前大部分以 Redis 为基础的分布式锁的问题

The simplest way to use Redis to lock a resource is to create a key in an instance. The key is usually created with a limited time to live, using the Redis expires feature, so that eventually it will get released (property 2 in our list). When the client needs to release the resource, it deletes the key.
用 Redis 来当锁， 最简单的方式就是在 Redis 实例中创建一个 key, 创建 key 的时候通常会给与这个 key 一个存活时间， 这通过 Redis 的过期机制就可以实现， 最终这个锁会被释放掉(我们列出的属性 2)。当客户端需要手动释放这个锁时， 只需要删除这个 key 就好

Superficially this works well, but there is a problem: this is a single point of failure in our architecture. What happens if the Redis master goes down? Well, let’s add a replica! And use it if the master is unavailable. This is unfortunately not viable. By doing so we can’t implement our safety property of mutual exclusion, because Redis replication is asynchronous.
表面上看这一切很不错， 但是这里有一个问题: 这将成为我们整个架构中的一个单点。 如果这个 Redis 实例挂掉了怎么办? 你可能会说， 添加一个备用节点! 如果主节点挂掉了那就让备用节点顶上。非常可惜，这行不通。 如果这么做我们将无法保证我们前面提到的安全性(同一时间只有一个客户端持有锁), 因为 Redis 的实例间的数据同步是异步的。

There is a race condition with this model:
如果这么做将会出现竞锁的情况:

Client A acquires the lock in the master.
客户端 A 从主节点那里获得了锁。

The master crashes before the write to the key is transmitted to the replica.
主节点在将数据同步到备用节点之前挂掉了

The replica gets promoted to master.
备用节点此时变成了主节点

Client B acquires the lock to the same resource A already holds a lock for. SAFETY VIOLATION!
客户端 B 此时获取同一资源的锁。冲突发生

Sometimes it is perfectly fine that, under special circumstances, for example during a failure, multiple clients can hold the lock at the same time. If this is the case, you can use your replication based solution. Otherwise we suggest to implement the solution described in this document.
有时候这无所谓， 在某些特定场景下，比如在有节点失效的情况下，多个客户端可以同时持有相同资源的锁。 如果你能接受这种情况， 那使用主备方案没什么问题。否则， 我们建议你采用我们接下来的方案。

### Correct Implementation with a Single Instance

### 单实例情况下正确实现

Before trying to overcome the limitation of the single instance setup described above, let’s check how to do it correctly in this simple case, since this is actually a viable solution in applications where a race condition from time to time is acceptable, and because locking into a single instance is the foundation we’ll use for the distributed algorithm described here.
在克服我们上面提到的单点问题之前， 让我们先来看一下如何正确实现单实例模式下的锁， 因为如果我们可以接受竞锁，这也是一种可行方案， 而且单实例锁的实现也是我们接下来要讲的方案的基础

To acquire the lock, the way to go is the following:
获取锁，我们只需要执行下面的命令:

    SET resource_name my_random_value NX PX 30000

The command will set the key only if it does not already exist (NX option), with an expire of 30000 milliseconds (PX option). The key is set to a value “my_random_value”. This value must be unique across all clients and all lock requests.
上面的命令会在 resource_name 不存在的情况下设置 resource_name 和 my_random_value(NX 参数), 同时设置 30000 毫秒的时间限制。这里的 my_random_value 必须要做到对所有客户端和获取锁的请求来说都是唯一的

Basically the random value is used in order to release the lock in a safe way, with a script that tells Redis: remove the key only if it exists and the value stored at the key is exactly the one I expect to be. This is accomplished by the following Lua script:
之所以要让这个值唯一，是因为我们需要用它来保证锁的安全释放， 下面的脚本告诉 Redis， 只有当 key 值存在且 value 与我们提供的值相同的情况下才能删除该 key

if redis.call("get",KEYS[1]) == ARGV[1] then
return redis.call("del",KEYS[1])
else
return 0
end

This is important in order to avoid removing a lock that was created by another client. For example a client may acquire the lock, get blocked performing some operation for longer than the lock validity time (the time at which the key will expire), and later remove the lock, that was already acquired by some other client. Using just DEL is not safe as a client may remove another client's lock. With the above script instead every lock is “signed” with a random string, so the lock will be removed only if it is still the one that was set by the client trying to remove it.
这对于防止误删其他客户端创建的锁很关键。 比如一个客户端获取了锁， 但是它对资源的操作超过了 key 的存活时长， 这时锁因为超时被释放了， 等它完成工作再释放这个锁的时候，它会释放掉别人获取的锁(因为对于某一资源，锁的 key 是相同的)。所以简单的 DEL 操作是无法保证锁安全释放的， 通过给每个锁(key)一个唯一的随机值，再利用上面的脚本来进行锁的释放，可以保证客户端只能释放它所获取的锁

What should this random string be? We assume it’s 20 bytes from /dev/urandom, but you can find cheaper ways to make it unique enough for your tasks. For example a safe pick is to seed RC4 with /dev/urandom, and generate a pseudo random stream from that. A simpler solution is to use a UNIX timestamp with microsecond precision, concatenating the timestamp with a client ID. It is not as safe, but probably sufficient for most environments.
那这个随机值该怎么取呢?我们可以每次取/dev/urandom 前 20 字节的， 当然， 你还可以通过其他更简单的方法来获取，只要保证在你的业务中唯一即可。例如， 把/dev/urandom 的值拿出来作为 RC4 的初始化种子， 然后用它来生成伪随机数。一个更简单的办法是利用 UNIX 时间戳精确到毫秒级别，将时间戳与客户端 id 进行拼合， 这不是绝对安全，但是对于大多数应用场景足够了。

The "lock validity time" is the time we use as the key's time to live. It is both the auto release time, and the time the client has in order to perform the operation required before another client may be able to acquire the lock again, without technically violating the mutual exclusion guarantee, which is only limited to a given window of time from the moment the lock is acquired.
锁的有效时长实际就是我们赋予 key 值的存活时长。 它即是自动释放的时长限制，同时也是客户端对资源进行操作的时间限制。

So now we have a good way to acquire and release the lock. With this system, reasoning about a non-distributed system composed of a single, always available, instance, is safe. Let’s extend the concept to a distributed system where we don’t have such guarantees.
现在我们完成了锁的设计， 该系统由一个实例组成，如果该实例不会挂掉，那这个系统就可以认为是安全的。现在让我们把该设计扩展成一个分布式的系统。

### The Redlock Algorithm

### Redlock 算法

In the distributed version of the algorithm we assume we have N Redis masters. Those nodes are totally independent, so we don’t use replication or any other implicit coordination system. We already described how to acquire and release the lock safely in a single instance. We take for granted that the algorithm will use this method to acquire and release the lock in a single instance. In our examples we set N=5, which is a reasonable value, so we need to run 5 Redis masters on different computers or virtual machines in order to ensure that they’ll fail in a mostly independent way.
在分布式的版本中，我们假设有 N 个 Redis 的主节点. 这些节点是完全独立的， 不会使用任何备用节点或者其他同步系统. 在前面我们已经描述了如何在单一节点上获取和释放锁, 在分布式版本中我们依然会沿用这一方式。在我们的例子中，我们把 N 设为 5， 这是一个比较合理的数字， 我们需要分别在 5 台物理机或者虚拟机上运行 5 个 Redis 的主节点， 以此保证如果他们其中的某几台挂掉，不会影响到其他节点的运行

In order to acquire the lock, the client performs the following operations:
为了获取锁， 客户端需要执行以下操作:

It gets the current time in milliseconds.
获取当前时间, 精确到毫秒

It tries to acquire the lock in all the N instances sequentially, using the same key name and random value in all the instances. During step 2, when setting the lock in each instance, the client uses a timeout which is small compared to the total lock auto-release time in order to acquire it. For example if the auto-release time is 10 seconds, the timeout could be in the ~ 5-50 milliseconds range. This prevents the client from remaining blocked for a long time trying to talk with a Redis node which is down: if an instance is not available, we should try to talk with the next instance ASAP.
用相同 key 和随机 value 依次向这 N 台实例发送获取锁的请求。在这个过程中， 客户端需要对请求加上超时限制， 该超时限制要远远小于锁的存活时间。假设自动释放时长为 10 秒， 那超时限制可以大约为 5 到 50 毫秒。这样可以防止客户端卡死在与挂掉的节点通讯上。如果某个节点没有响应， 我们应该尽快的去请求下一节点。

The client computes how much time elapsed in order to acquire the lock, by subtracting from the current time the timestamp obtained in step 1. If and only if the client was able to acquire the lock in the majority of the instances (at least 3), and the total time elapsed to acquire the lock is less than lock validity time, the lock is considered to be acquired.
客户端在请求锁的时候需要计算当前已经流逝的时间， 这个可以通过用当前时间减去步骤 1 获取到的时间来得到。只有客户端成功从半数以上的实例(在本例中为 3)获取成功， 且获取的总耗时小于锁的有效时长， 才被认为是成功获取了锁。

If the lock was acquired, its validity time is considered to be the initial validity time minus the time elapsed, as computed in step 3.
如果锁成功获取了， 那么它实际的有效时长则为初始有效时长减去获取锁过程中的时间消耗， 就像步骤 3 中计算的那样。

If the client failed to acquire the lock for some reason (either it was not able to lock N/2+1 instances or the validity time is negative), it will try to unlock all the instances (even the instances it believed it was not able to lock).
如果客户端因为某种原因获取锁失败了(不管是没有成功获取半数还是实际有效时长变成了负数), 它都要在所有的节点上释放锁， 不管该节点是不是获取成功

### Is the Algorithm Asynchronous?

### 该算法是异步的吗?

The algorithm relies on the assumption that while there is no synchronized clock across the processes, the local time in every process updates at approximately at the same rate, with a small margin of error compared to the auto-release time of the lock. This assumption closely resembles a real-world computer: every computer has a local clock and we can usually rely on different computers to have a clock drift which is small.
这个算法建立在一个假设之上， 那就是整个获取过程中没有可用的同步时钟， 且每个获取过程中， 本地时间都在以相同的速率更新(这地方有点拗口， 我的理解是， 我们没有一个所有节点和客户端都认可的同步时钟可供参考， 但是整个获取锁的过程中因为我们都是在同一个客户端上进行操作，所以时长是可信的， 因为一台机器的时钟不会无缘无故的变快或者变慢太多， 所以时间点不可信，但是时长可信), 所以本地时间相对于自动释放时间的误差可以忽略不计。该假设比较符合现实中的计算机系统: 每台计算机都有一个本地时钟并且我们通常依赖于不同计算机之间的时钟漂移差(clock drift 我也不知道该怎么翻译，应该指的是不同的两个时钟之间的走速差异)不大

At this point we need to better specify our mutual exclusion rule: it is guaranteed only as long as the client holding the lock terminates its work within the lock validity time (as obtained in step 3), minus some time (just a few milliseconds in order to compensate for clock drift between processes).
现在是时候进一步解释一下我们的互斥独占规则了: 互斥性只有在客户端获取到锁且在锁的有效时长之内完成它的工作才能得到保证， 这个有效时长是指的从步骤 3 中计算得到的时长然后减去几毫秒(补偿不同获取过程之间的 clock drift)得到的时长

This paper contains more information about similar systems requiring a bound clock drift: Leases: an efficient fault-tolerant mechanism for distributed file cache consistency.
如果想进一步了解 clock drift, 建议读一下这篇文章

### Retry on Failure

### 重试

When a client is unable to acquire the lock, it should try again after a random delay in order to try to desynchronize multiple clients trying to acquire the lock for the same resource at the same time (this may result in a split brain condition where nobody wins). Also the faster a client tries to acquire the lock in the majority of Redis instances, the smaller the window for a split brain condition (and the need for a retry), so ideally the client should try to send the SET commands to the N instances at the same time using multiplexing.
当客户端无法获取到锁， 它应该在一段随机延迟之后重试， 之所以加随机延迟是为了能防止多个客户端同时获取一个锁的情况发生(这有可能会导致脑裂情况的发生). 客户端越快的获取到半数以上， 脑裂发生的时间窗口也就越小， 需要重试的可能性也就越小， 所以理想的客户端应当使用多路复用同时向 N 个节点发送 SET 命令。

It is worth stressing how important it is for clients that fail to acquire the majority of locks, to release the (partially) acquired locks ASAP, so that there is no need to wait for key expiry in order for the lock to be acquired again (however if a network partition happens and the client is no longer able to communicate with the Redis instances, there is an availability penalty to pay as it waits for key expiration).

Releasing the Lock
Releasing the lock is simple, and can be performed whether or not the client believes it was able to successfully lock a given instance.

Safety Arguments
Is the algorithm safe? Let's examine what happens in different scenarios.

To start let’s assume that a client is able to acquire the lock in the majority of instances. All the instances will contain a key with the same time to live. However, the key was set at different times, so the keys will also expire at different times. But if the first key was set at worst at time T1 (the time we sample before contacting the first server) and the last key was set at worst at time T2 (the time we obtained the reply from the last server), we are sure that the first key to expire in the set will exist for at least MIN_VALIDITY=TTL-(T2-T1)-CLOCK_DRIFT. All the other keys will expire later, so we are sure that the keys will be simultaneously set for at least this time.

During the time that the majority of keys are set, another client will not be able to acquire the lock, since N/2+1 SET NX operations can’t succeed if N/2+1 keys already exist. So if a lock was acquired, it is not possible to re-acquire it at the same time (violating the mutual exclusion property).

However we want to also make sure that multiple clients trying to acquire the lock at the same time can’t simultaneously succeed.

If a client locked the majority of instances using a time near, or greater, than the lock maximum validity time (the TTL we use for SET basically), it will consider the lock invalid and will unlock the instances, so we only need to consider the case where a client was able to lock the majority of instances in a time which is less than the validity time. In this case for the argument already expressed above, for MIN_VALIDITY no client should be able to re-acquire the lock. So multiple clients will be able to lock N/2+1 instances at the same time (with "time" being the end of Step 2) only when the time to lock the majority was greater than the TTL time, making the lock invalid.

Liveness Arguments
The system liveness is based on three main features:

The auto release of the lock (since keys expire): eventually keys are available again to be locked.
The fact that clients, usually, will cooperate removing the locks when the lock was not acquired, or when the lock was acquired and the work terminated, making it likely that we don’t have to wait for keys to expire to re-acquire the lock.
The fact that when a client needs to retry a lock, it waits a time which is comparably greater than the time needed to acquire the majority of locks, in order to probabilistically make split brain conditions during resource contention unlikely.
However, we pay an availability penalty equal to TTL time on network partitions, so if there are continuous partitions, we can pay this penalty indefinitely. This happens every time a client acquires a lock and gets partitioned away before being able to remove the lock.

Basically if there are infinite continuous network partitions, the system may become not available for an infinite amount of time.

Performance, Crash Recovery and fsync
Many users using Redis as a lock server need high performance in terms of both latency to acquire and release a lock, and number of acquire / release operations that it is possible to perform per second. In order to meet this requirement, the strategy to talk with the N Redis servers to reduce latency is definitely multiplexing (putting the socket in non-blocking mode, send all the commands, and read all the commands later, assuming that the RTT between the client and each instance is similar).

However there is another consideration around persistence if we want to target a crash-recovery system model.

Basically to see the problem here, let’s assume we configure Redis without persistence at all. A client acquires the lock in 3 of 5 instances. One of the instances where the client was able to acquire the lock is restarted, at this point there are again 3 instances that we can lock for the same resource, and another client can lock it again, violating the safety property of exclusivity of lock.

If we enable AOF persistence, things will improve quite a bit. For example we can upgrade a server by sending it a SHUTDOWN command and restarting it. Because Redis expires are semantically implemented so that time still elapses when the server is off, all our requirements are fine. However everything is fine as long as it is a clean shutdown. What about a power outage? If Redis is configured, as by default, to fsync on disk every second, it is possible that after a restart our key is missing. In theory, if we want to guarantee the lock safety in the face of any kind of instance restart, we need to enable fsync=always in the persistence settings. This will affect performance due to the additional sync overhead.

However things are better than they look like at a first glance. Basically, the algorithm safety is retained as long as when an instance restarts after a crash, it no longer participates to any currently active lock. This means that the set of currently active locks when the instance restarts were all obtained by locking instances other than the one which is rejoining the system.

To guarantee this we just need to make an instance, after a crash, unavailable for at least a bit more than the max TTL we use. This is the time needed for all the keys about the locks that existed when the instance crashed to become invalid and be automatically released.

Using delayed restarts it is basically possible to achieve safety even without any kind of Redis persistence available, however note that this may translate into an availability penalty. For example if a majority of instances crash, the system will become globally unavailable for TTL (here globally means that no resource at all will be lockable during this time).

Making the algorithm more reliable: Extending the lock
If the work performed by clients consists of small steps, it is possible to use smaller lock validity times by default, and extend the algorithm implementing a lock extension mechanism. Basically the client, if in the middle of the computation while the lock validity is approaching a low value, may extend the lock by sending a Lua script to all the instances that extends the TTL of the key if the key exists and its value is still the random value the client assigned when the lock was acquired.

The client should only consider the lock re-acquired if it was able to extend the lock into the majority of instances, and within the validity time (basically the algorithm to use is very similar to the one used when acquiring the lock).

However this does not technically change the algorithm, so the maximum number of lock reacquisition attempts should be limited, otherwise one of the liveness properties is violated.
