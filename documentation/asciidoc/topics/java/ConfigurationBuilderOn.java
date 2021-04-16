import org.infinispan.client.hotrod.configuration.ConfigurationBuilder;
import org.infinispan.client.hotrod.configuration.SaslQop;
import org.infinispan.client.hotrod.impl.ConfigurationProperties;
...

ConfigurationBuilder builder = new ConfigurationBuilder();
      builder.addServer()
               .host("$SERVICE_HOSTNAME")
               .port(ConfigurationProperties.DEFAULT_HOTROD_PORT)
             .security().authentication()
               .username("username")
               .password("password")
               .realm("default")
               .saslQop(SaslQop.AUTH)
               .saslMechanism("SCRAM-SHA-512")
             .ssl()
               .sniHostName("$SERVICE_HOSTNAME")
               .trustStorePath("/var/run/secrets/kubernetes.io/serviceaccount/service-ca.crt");
