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
               .trustStorePath("/path/to/tls.crt");
      builder.clientIntelligence(ClientIntelligence.BASIC);
