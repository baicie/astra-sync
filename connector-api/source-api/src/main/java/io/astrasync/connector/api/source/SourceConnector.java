package io.astrasync.connector.api.source;

import io.astrasync.connector.api.*;
import io.astrasync.connector.api.data.*;
import io.astrasync.connector.api.metadata.*;

import java.util.*;

public interface SourceConnector extends Configurable {

    ConnectorCapabilities capabilities();

    TableCatalog catalog();

    SplitEnumerator createEnumerator(SourceContext context);

    SourceReader createReader(ReaderContext context);
}
