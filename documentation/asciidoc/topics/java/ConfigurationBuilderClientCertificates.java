import org.infinispan.client.hotrod.configuration.ConfigurationBuilder;
...

ConfigurationBuilder builder = new ConfigurationBuilder();
      builder.security()
             .authentication()
               .saslMechanism("EXTERNAL")
             .ssl()
               .keyStoreFileName("/path/to/keystore")
               .keyStorePassword("keystorepassword".toCharArray())
               .keyStoreType("PCKS12");
