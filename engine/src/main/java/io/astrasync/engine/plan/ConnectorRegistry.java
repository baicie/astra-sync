package io.astrasync.engine.plan;

import io.astrasync.connector.api.ConnectorDescriptor;
import io.astrasync.connector.api.ConnectorFactory;
import java.util.Arrays;
import java.util.Collection;
import java.util.List;
import java.util.NavigableMap;
import java.util.Objects;
import java.util.Optional;
import java.util.ServiceLoader;
import java.util.TreeMap;

public final class ConnectorRegistry {
    private final NavigableMap<String, Entry> entries;

    public ConnectorRegistry(Collection<? extends ConnectorFactory> factories) {
        Objects.requireNonNull(factories, "factories must not be null");
        TreeMap<String, Entry> registered = new TreeMap<>();
        for (ConnectorFactory factory : factories) {
            ConnectorFactory checkedFactory = Objects.requireNonNull(factory, "factory must not be null");
            ConnectorDescriptor descriptor =
                    Objects.requireNonNull(checkedFactory.descriptor(), "descriptor must not be null");
            Entry previous = registered.putIfAbsent(descriptor.name(), new Entry(descriptor, checkedFactory));
            if (previous != null) {
                throw new IllegalArgumentException("duplicate connector name: " + descriptor.name());
            }
        }
        entries = java.util.Collections.unmodifiableNavigableMap(registered);
    }

    public static ConnectorRegistry of(ConnectorFactory... factories) {
        Objects.requireNonNull(factories, "factories must not be null");
        return new ConnectorRegistry(Arrays.asList(factories));
    }

    public static ConnectorRegistry discover() {
        return discover(Thread.currentThread().getContextClassLoader());
    }

    public static ConnectorRegistry discover(ClassLoader classLoader) {
        Objects.requireNonNull(classLoader, "classLoader must not be null");
        List<ConnectorFactory> factories = ServiceLoader.load(ConnectorFactory.class, classLoader).stream()
                .map(ServiceLoader.Provider::get)
                .toList();
        return new ConnectorRegistry(factories);
    }

    public Optional<ConnectorDescriptor> findDescriptor(String name) {
        Entry entry = entries.get(name);
        return entry == null ? Optional.empty() : Optional.of(entry.descriptor());
    }

    public Optional<ConnectorFactory> findFactory(String name) {
        Entry entry = entries.get(name);
        return entry == null ? Optional.empty() : Optional.of(entry.factory());
    }

    public ConnectorFactory requireFactory(String name, String version) {
        Entry entry = entries.get(name);
        if (entry == null) {
            throw new IllegalArgumentException("connector is not registered: " + name);
        }
        if (!entry.descriptor().version().equals(version)) {
            throw new IllegalStateException("connector version changed for " + name + ": expected " + version
                    + ", registered " + entry.descriptor().version());
        }
        return entry.factory();
    }

    public List<ConnectorDescriptor> descriptors() {
        return entries.values().stream().map(Entry::descriptor).toList();
    }

    private record Entry(ConnectorDescriptor descriptor, ConnectorFactory factory) {}
}
