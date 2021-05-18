import org.infinispan.client.hotrod.configuration.ClientIntelligence;
import org.infinispan.client.hotrod.configuration.ConfigurationBuilder;
import org.infinispan.client.hotrod.configuration.SaslQop;
...

ConfigurationBuilder builder = new ConfigurationBuilder();
      builder.addServer()
               .host("$SERVICE_HOSTNAME")
               .port("$PORT")
             .security().authentication()
               .username("username")
               .password("password")
               .realm("default")
               .saslQop(SaslQop.AUTH)
               .saslMechanism("SCRAM-SHA-512")
             .ssl()
               .sniHostName("$SERVICE_HOSTNAME")
               //Create a client trust store with tls.crt from your project.
               .trustStoreFileName("/path/to/truststore.pkcs12")
               .trustStorePassword("trust_store_password")
               .trustStoreType("PCKS12");
      builder.clientIntelligence(ClientIntelligence.BASIC);
